#pragma once
// Recovery runs under control.lock and startup.lock after a full explicit stop.
// Never use executable path equality or a textual "stopped" flag as exit proof.
inline void nativeRequireExited(const json& identity,bool neverStartedWorker=false){
 if(neverStartedWorker&&identity.is_object()&&identity.size()==1&&identity.contains("running")&&identity["running"].is_boolean()&&!identity["running"].get<bool>())return;
 if(!identity.is_object()||!identity.contains("pid")||!identity["pid"].is_number_unsigned()||!identity.contains("created")||!identity["created"].is_number_unsigned())throw std::runtime_error("Stop recovery refused: invalid process lifetime record");
 auto pid=identity["pid"].get<uint64_t>();if(!pid||pid>MAXDWORD||!identity["created"].get<uint64_t>())throw std::runtime_error("Stop recovery refused: invalid process lifetime identity");
 Handle process(OpenProcess(SYNCHRONIZE|PROCESS_QUERY_LIMITED_INFORMATION,FALSE,DWORD(pid)));
 if(!process.valid()){auto error=GetLastError();if(error==ERROR_INVALID_PARAMETER)return;throw winFailure("Stop recovery cannot inspect previous process",error);}
 if(nativeIdentity(process.h,DWORD(pid))["created"]!=identity["created"])return; // PID reused; do not touch its new owner.
 auto result=WaitForSingleObject(process.h,0);if(result==WAIT_OBJECT_0)return;
 if(result==WAIT_FAILED)throw winFailure("Stop recovery cannot observe previous process",GetLastError());
 throw std::runtime_error("Stop recovery refused: an exact previous process lifetime is still active");
}
inline bool nativeKnownJobBlocker(const std::string& reason){
 return reason=="Native child termination unconfirmed; action stopped"||reason=="Native process tree termination unconfirmed"||reason=="Native process tree still active; action stopped"||reason=="Native host job could not stop"||reason=="Native host process tree termination is unconfirmed"||reason=="Owned native host process tree did not confirm exit"||
  std::regex_match(reason,std::regex("Native process tree termination unconfirmed \\(Windows error [0-9]+, observation error [0-9]+\\); stop required"));
}
// A later recoverable Job error must never replace an earlier unknown/update
// blocker. Exclusive creation also closes the concurrent first-writer race.
inline void nativeRecordStopBlocker(const std::string& reason){
 auto path=stateRoot/L"host-stop-required";realPath(path);auto message=aide_host::diagnostic(reason);
 if(!nativeKnownJobBlocker(message)){write(path,message);return;}
 if(fs::exists(path))return;
 try{write(path,message,false);}catch(...){if(fs::exists(path))return;throw;}
}
inline void nativeRecoverStopBlocker(Launcher& app,bool explicitRecovery){
 auto blocker=stateRoot/L"host-stop-required";realPath(blocker);if(!fs::exists(blocker)){if(explicitRecovery)app.say("No native stop blocker remains.");return;}
 auto original=read(blocker,8192);auto reason=trim(original);auto record=json::parse(original,nullptr,false);
 auto status=nativeControllerStatus();bool directory=record.is_object()&&record.value("source","")=="directory-apply";
 if(!nativeKnownJobBlocker(reason)&&!directory){if(explicitRecovery)throw std::runtime_error("Stop recovery refused: unknown blocker; inspect host-status and the original marker");app.say("Stop completed; unknown blocker retained for inspection (recover-stop).");return;}
 for(auto path:{stateRoot/L"update/transaction.json",app.root/L".aide-data/update-agent/launcher-stop-required",app.root/L".aide-data/update-agent/transaction.json"}){realPath(path);if(fs::exists(path))throw std::runtime_error("Stop recovery refused: update transaction or previous unsafe update marker remains");}
 privateDirectory(app.root/L".aide-update-agent.lock");
 NativeLock controller(stateRoot/L"controller.lock"),previous(app.root/L".aide-update-agent.lock/owner.lock");
 if(!controller.owns()||!previous.owns())throw std::runtime_error("Stop recovery refused: a controller or previous update agent still owns this installation");
 if(fs::exists(stateRoot/L"host-status.json"))nativeRequireExited(status);
 if(status.contains("workers")){if(!status["workers"].is_object())throw std::runtime_error("Stop recovery refused: invalid worker records");for(const auto& worker:status["workers"])nativeRequireExited(worker,true);}
 for(auto role:{L"port",L"update"}){auto path=stateRoot/(std::wstring(L"task-")+role+L".json");realPath(path);if(fs::exists(path))nativeRequireExited(parseJson(read(path,16384)));}
 if(directory&&(record.value("controllerGeneration","").empty()||record.value("controllerGeneration","")!=status.value("generation","")||status.value("state","")!="stopped"))throw std::runtime_error("Stop recovery refused: directory blocker controller generation is not confirmed stopped");
 // Reinspect every project container after compose stop. A successful CLI exit
 // alone does not establish that there are no running/paused/restarting owners.
 auto project=app.config().value("name","");if(!std::regex_match(project,std::regex("[a-z0-9][a-z0-9_-]*")))throw std::runtime_error("Stop recovery refused: invalid Compose project");
 auto containers=app.d({L"ps",L"-aq",L"--no-trunc",L"--filter",wide("label=com.docker.compose.project="+project)});app.required(containers,"Stop recovery cannot list project containers");
 std::istringstream ids(containers.out);std::string id;unsigned count=0;while(ids>>id){
  if(!digest(id))throw std::runtime_error("Stop recovery refused: invalid container identity");auto response=app.d({L"inspect",wide(id),L"--format",L"{{json .}}"});app.required(response,"Stop recovery cannot inspect project container");auto value=parseJson(response.out);
  if(label(value,"com.docker.compose.project")!=project||!aide_installation::owns(app.root,label(value,"com.docker.compose.project.working_dir")))throw std::runtime_error("Stop recovery refused: unrelated project container");
  auto state=value.at("State");auto phase=state.value("Status","");if(state.value("Running",true)||state.value("Paused",true)||state.value("Restarting",true)||state.value("Pid",uint64_t(1))!=0||(phase!="exited"&&phase!="created"))throw std::runtime_error("Stop recovery refused: project container has not confirmed exit");count++;
 }
 auto temporary=app.d({L"ps",L"-aq",L"--no-trunc",L"--filter",wide("label=aide.native.root="+app.nativeRoot())});app.required(temporary,"Stop recovery cannot inspect temporary containers");if(!trim(temporary.out).empty())throw std::runtime_error("Stop recovery refused: temporary containers remain");
 auto suffix=L".recovered-"+std::to_wstring(GetCurrentProcessId())+L"-"+std::to_wstring(GetTickCount64());auto archive=blocker;archive+=suffix;auto evidence=archive;evidence+=L".json";
 // Audit first; never remove the blocker if evidence cannot be durably saved.
 write(evidence,json({{"version",1},{"root",utf8(fs::weakly_canonical(app.root).wstring())},{"project",project},{"projectContainersStopped",count},{"controllerAndWorkersExited",true},{"updateTransactionsAbsent",true},{"temporaryContainersAbsent",true},{"originalMarkerSha256",sha256(blocker)}}).dump(2));
 if(read(blocker,8192)!=original)throw std::runtime_error("Stop recovery refused: blocker changed during inspection");
 moveState(blocker,archive,MOVEFILE_WRITE_THROUGH,"Cannot archive recovered native stop marker");
 app.say("Verified full stop; previous native stop blocker archived. Start can now be retried.");
}
