#pragma once
// One distributed executable, isolated user-session tasks, one owned process job.
inline json nativeIdentity(HANDLE process,DWORD pid){FILETIME created{},exited{},kernel{},user{};if(!GetProcessTimes(process,&created,&exited,&kernel,&user))throw std::runtime_error("Cannot identify native process lifetime");return {{"pid",pid},{"created",(uint64_t(created.dwHighDateTime)<<32)|created.dwLowDateTime}};}
inline bool nativeAlive(const json& identity){
 if(!identity.is_object()||!identity.contains("pid")||!identity["pid"].is_number_unsigned()||!identity.contains("created")||!identity["created"].is_number_unsigned())return false;
 auto number=identity["pid"].get<uint64_t>();if(!number||number>MAXDWORD)return false;Handle process(OpenProcess(SYNCHRONIZE|PROCESS_QUERY_LIMITED_INFORMATION,FALSE,DWORD(number)));if(!process.valid()||WaitForSingleObject(process.h,0)!=WAIT_TIMEOUT)return false;
 if(nativeIdentity(process.h,DWORD(number))["created"]!=identity["created"])return false;std::wstring name(32768,0);DWORD size=DWORD(name.size());if(!QueryFullProcessImageNameW(process.h,0,name.data(),&size))return false;name.resize(size);std::error_code error;return fs::equivalent(fs::path(name),module(),error)&&!error;
}
inline json nativeControllerStatus(){auto path=stateRoot/L"host-status.json";realPath(path);if(!fs::exists(path))return {{"running",false}};auto value=parseJson(read(path,16384));if(!value.is_object()||value.value("version",0)!=1||!aide_installation::owns(stateRoot.parent_path().parent_path(),value.value("root","")))throw std::runtime_error("Invalid or unrelated native controller status");value["running"]=nativeAlive(value);return value;}
inline void nativeTaskStatus(const std::string& role,const std::string& phase,const std::string& message={}){auto value=nativeIdentity(GetCurrentProcess(),GetCurrentProcessId());value["role"]=role;value["phase"]=phase;value["message"]=aide_host::diagnostic(message);write(stateRoot/wide("task-"+role+".json"),value.dump());}
struct NativeWorker {
 std::string role;std::unique_ptr<Handle> process;json identity;ULONGLONG retryAt=0;unsigned failures=0;
 explicit NativeWorker(std::string r):role(std::move(r)){}
 bool alive(){return process&&WaitForSingleObject(process->h,0)==WAIT_TIMEOUT;}
 void start(HANDLE job,const fs::path& root,const std::string& generation){
  auto log=stateRoot/wide("task-"+role+".log");realPath(log);if(fs::exists(log)&&fs::file_size(log)>1024*1024){auto previous=log;previous+=L".1";realPath(previous);fs::remove(previous);fs::rename(log,previous);}
  SECURITY_ATTRIBUTES security{sizeof(security),nullptr,TRUE};Handle output(CreateFileW(log.c_str(),FILE_APPEND_DATA,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,&security,OPEN_ALWAYS,FILE_ATTRIBUTE_NORMAL,nullptr));Handle input(CreateFileW(L"NUL",GENERIC_READ,FILE_SHARE_READ|FILE_SHARE_WRITE,&security,OPEN_EXISTING,0,nullptr));if(!output.valid()||!input.valid())throw std::runtime_error("Cannot open owned worker handles");
  auto program=module();std::wstring line=quote(program.wstring())+L" "+quote(wide("worker-"+role))+L" --root "+quote(root.wstring())+L" --generation "+quote(wide(generation))+L" --no-browser";Startup startup(input.h,output.h,output.h);PROCESS_INFORMATION info{};
  if(!CreateProcessW(program.c_str(),line.data(),nullptr,nullptr,TRUE,CREATE_SUSPENDED|CREATE_NO_WINDOW|EXTENDED_STARTUPINFO_PRESENT,nullptr,nullptr,&startup.info.StartupInfo,&info))throw std::runtime_error("Cannot start native worker");Handle thread(info.hThread);auto next=std::make_unique<Handle>(info.hProcess);
  if(!AssignProcessToJobObject(job,next->h)||ResumeThread(thread.h)==DWORD(-1)){TerminateProcess(next->h,125);if(WaitForSingleObject(next->h,5000)!=WAIT_OBJECT_0)throw aide_host::Unsafe("Native worker termination is unconfirmed");throw std::runtime_error("Cannot attach native worker to its owned job");}
  identity=nativeIdentity(next->h,info.dwProcessId);process=std::move(next);
 }
};
inline bool nativeStopping(){return fs::exists(stateRoot/L"stop");}
inline bool nativeControllerMatches(const std::string& generation){auto status=nativeControllerStatus();return status.value("running",false)&&status.value("generation","")==generation;}
inline int nativeWorkerLoop(Launcher& app,const std::string& role,const std::string& generation){
 bool warmed=false,seeded=false;std::string lastRefresh;auto nextCheck=GetTickCount64();app.allowDesktop=false;
 for(int i=0;i<20&&!nativeControllerMatches(generation)&&!nativeStopping();i++)Sleep(100);
 if(!nativeControllerMatches(generation))throw std::runtime_error("Native worker requires its live controller generation");
 observeNativeStop=true;nativeStopPath=stateRoot/L"stop";setenvw(L"AIDE_NATIVE_CONTROLLER_GENERATION",wide(generation));
 while(!nativeStopping()&&nativeControllerMatches(generation)){
  if(GetTickCount64()<nextCheck){Sleep(200);continue;}nextCheck=GetTickCount64()+2000;
  try{
   NativeLock lifecycle(stateRoot/L"startup.lock");if(!lifecycle.owns()){nativeTaskStatus(role,"waiting","Another lifecycle operation owns this installation");continue;}
   if(nativeStopping())break;
   auto blocker=stateRoot/L"host-stop-required";realPath(blocker);if(fs::exists(blocker)){nativeTaskStatus(role,"blocked","A previous host action needs a confirmed stop");continue;}
   SetEnvironmentVariableW(L"AIDE_PORT",nullptr);SetEnvironmentVariableW(L"COMPOSE_PROJECT_NAME",nullptr);app.restore();app.engine(false);nativeTaskStatus(role,"working");
   auto cid=app.container();if(cid.empty()){nativeTaskStatus(role,"waiting","Application is not running");continue;}
   if(role=="port"){
    auto actual=app.d({L"inspect",wide(cid),L"--format",L"{{.Image}}"});app.required(actual,"Cannot resolve current helper runtime");app.image=trim(actual.out);if(!imageId(app.image))throw std::runtime_error("Invalid running image identity");setenvw(L"AIDE_IMAGE",wide(app.image));
    auto requested=stateRoot/L"helper-refresh";realPath(requested);auto refresh=fs::exists(requested)?read(requested,256):"initial";
    if(!warmed||refresh!=lastRefresh){app.prepareDirectory();app.helpers();warmed=true;lastRefresh=refresh;write(stateRoot/L"helper-ready",refresh);}
    auto desired=app.compose({L"exec",L"-T",L"aide",L"cat",L"/data/config/host-port"});auto value=trim(desired.out);int current=app.mappedPort();
    if(!desired.code&&std::regex_match(value,std::regex("[0-9]{1,5}"))){int requested=std::stoi(value);if(requested>0&&requested<=65535&&current&&requested!=current&&portFree(requested)){
     auto file=app.root/L".env";bool existed=fs::exists(file);auto previous=existed?read(file):"";auto processPort=env(L"AIDE_PORT");
     try{setting(app.root,"AIDE_PORT",value);setenvw(L"AIDE_PORT",wide(value));if(nativeStopping())break;app.prepareDirectory();app.required(app.compose({L"up",L"-d",L"--no-build",L"--pull",L"never"},300000),"Native port recreation failed");app.waitHealthy();app.helpers();app.save();}
     catch(const aide_host::Unsafe&){throw;}
     catch(...){if(existed)write(file,previous);else fs::remove(file);if(processPort.empty())SetEnvironmentVariableW(L"AIDE_PORT",nullptr);else setenvw(L"AIDE_PORT",processPort);if(!nativeStopping()){app.required(app.compose({L"up",L"-d",L"--no-build",L"--pull",L"never"},300000),"Native previous port could not recover");app.waitHealthy();app.helpers();}throw;}
    }}
   }else if(role=="update"&&(fs::exists(app.root/L".aide-image")||!fs::exists(app.root/L"Dockerfile"))){
    NativeUpdateHost host(app);if(!host.legacyResult()){nativeTaskStatus(role,"waiting","Retrying the durable previous update result");continue;}
    if(!seeded&&host.load().is_null()){if(!host.seed()){nativeTaskStatus(role,"not-applicable","Source runtime does not support application A/B updates");continue;}seeded=true;}
    aide_host::Supervisor<NativeUpdateHost>(host).tick();auto transaction=host.load();if(transaction.is_object()&&transaction.value("phase","")=="blocked"){write(stateRoot/L"host-stop-required",transaction.value("reason","Native update needs inspection"));nativeTaskStatus(role,"blocked",transaction.value("reason","Native update needs inspection"));continue;}
   }
   nativeTaskStatus(role,"idle");
  }catch(const aide_host::Unsafe& e){write(stateRoot/L"host-stop-required",aide_host::diagnostic(e.what()));nativeTaskStatus(role,"blocked",e.what());}
   catch(const std::exception& e){if(nativeStopping())break;nativeTaskStatus(role,"retry",e.what());}
 }
 nativeTaskStatus(role,"stopped");return 0;
}
inline int nativeSupervise(Launcher& app){
 NativeLock controller(stateRoot/L"controller.lock");if(!controller.owns())return 0;
 privateDirectory(app.root/L".aide-update-agent.lock");NativeLock previous(app.root/L".aide-update-agent.lock/owner.lock");if(!previous.owns())throw std::runtime_error("The previous host update agent is still active; stop it before native supervision");
 Handle job(CreateJobObjectW(nullptr,nullptr));JOBOBJECT_EXTENDED_LIMIT_INFORMATION limits{};limits.BasicLimitInformation.LimitFlags=JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE|JOB_OBJECT_LIMIT_ACTIVE_PROCESS;limits.BasicLimitInformation.ActiveProcessLimit=32;
 if(!job.valid()||!SetInformationJobObject(job.h,JobObjectExtendedLimitInformation,&limits,sizeof(limits)))throw std::runtime_error("Cannot create the bounded native host job");
 std::array<unsigned char,16> bytes{};if(BCryptGenRandom(nullptr,bytes.data(),ULONG(bytes.size()),BCRYPT_USE_SYSTEM_PREFERRED_RNG)<0)throw std::runtime_error("Cannot create native controller identity");std::string generation;const char* hex="0123456789abcdef";for(auto c:bytes){generation+=hex[c>>4];generation+=hex[c&15];}
 auto status=nativeIdentity(GetCurrentProcess(),GetCurrentProcessId());status["version"]=1;status["generation"]=generation;status["root"]=utf8(fs::weakly_canonical(app.root).wstring());status["state"]="starting";write(stateRoot/L"host-status.json",status.dump());
 NativeWorker port("port"),update("update");std::array<NativeWorker*,2> workers{&port,&update};
 while(!nativeStopping()){
  for(auto worker:workers)if(!worker->alive()&&GetTickCount64()>=worker->retryAt){
   if(worker->process){worker->failures++;worker->retryAt=GetTickCount64()+std::min<unsigned>(60000,1000u<<std::min<unsigned>(worker->failures,6));worker->process.reset();continue;}
   try{worker->start(job.h,app.root,generation);}catch(const aide_host::Unsafe& e){write(stateRoot/L"host-stop-required",aide_host::diagnostic(e.what()));throw;}catch(const std::exception& e){nativeTaskStatus(worker->role,"retry",e.what());worker->retryAt=GetTickCount64()+3000;}
  }
  status["state"]="running";status["workers"]=json::object();for(auto worker:workers){auto record=worker->identity;record["running"]=worker->alive();status["workers"][worker->role]=record;}
  write(stateRoot/L"host-status.json",status.dump());Sleep(500);
 }
 if(!TerminateJobObject(job.h,0))throw aide_host::Unsafe("Native host job could not stop");
 bool stopped=false;for(int i=0;i<100;i++){JOBOBJECT_BASIC_ACCOUNTING_INFORMATION accounting{};if(!QueryInformationJobObject(job.h,JobObjectBasicAccountingInformation,&accounting,sizeof(accounting),nullptr))break;if(!accounting.ActiveProcesses){stopped=true;break;}Sleep(100);}
 if(!stopped){write(stateRoot/L"host-stop-required","Owned native host process tree did not confirm exit");throw aide_host::Unsafe("Native host process tree termination is unconfirmed");}
 status["state"]="stopped";write(stateRoot/L"host-status.json",status.dump());return 0;
}
inline void nativeStopController(){
 write(stateRoot/L"stop","");auto status=nativeControllerStatus();if(!status.value("running",false))return;
 for(int i=0;i<150;i++){if(!nativeAlive(status))return;Sleep(100);}throw aide_host::Unsafe("Native controller did not stop; lifecycle action refused");
}
// Only the known directory timeout marker can be cleared by a verified full
// stop. Unknown child/legacy update blockers still require operator inspection.
inline void nativeClearDirectoryBlocker(){
 auto path=stateRoot/L"host-stop-required";realPath(path);if(!fs::exists(path))return;
 auto value=json::parse(read(path,8192),nullptr,false);if(!value.is_object()||value.value("source","")!="directory-apply")return;
 auto status=nativeControllerStatus();if(status.value("running",false)||status.value("state","")!="stopped"||value.value("controllerGeneration","").empty()||value.value("controllerGeneration","")!=status.value("generation",""))return;
 if(fs::exists(stateRoot/L"update/transaction.json"))return;
 fs::remove(path);
}
inline void nativeLaunchController(Launcher& app){
 auto current=nativeControllerStatus();if(!current.value("running",false))spawn(module(),{L"supervise",L"--root",app.root.wstring(),L"--no-browser"},stateRoot/L"host-supervisor.log");
 bool ready=false;for(int i=0;i<100;i++){auto status=nativeControllerStatus();if(status.value("running",false)&&status.value("state","")=="running"){ready=true;break;}Sleep(100);}
 if(!ready)throw std::runtime_error("Native background controller did not become ready; inspect host-supervisor.log");
 for(int i=0;i<300&&!nativeStopping();i++){auto requested=stateRoot/L"helper-refresh",available=stateRoot/L"helper-ready";realPath(requested);realPath(available);if(fs::exists(requested)&&fs::exists(available)&&read(requested,256)==read(available,256))return;Sleep(100);}
 throw std::runtime_error("Native controller is running; embedded host components did not confirm readiness");
}
inline void nativeCheckPreviousAgent(Launcher& app){
 auto oldBlocker=app.root/L".aide-data/update-agent/launcher-stop-required";realPath(oldBlocker);if(fs::exists(oldBlocker))throw std::runtime_error("A previous launcher termination remains unconfirmed; inspect the existing blocker before startup");
 if(fs::exists(stateRoot/L"host-stop-required"))throw std::runtime_error("A native host action needs a confirmed stop; inspect host-status before startup");
 if(nativeControllerStatus().value("running",false))return;
 privateDirectory(app.root/L".aide-update-agent.lock");NativeLock previous(app.root/L".aide-update-agent.lock/owner.lock");if(!previous.owns())throw std::runtime_error("A previous update agent still owns this installation; close it before switching to native mode");
}
inline void nativeRestoreInterruptedMarker(Launcher& app){
 auto path=stateRoot/L"update/transaction.json";realPath(path);if(!fs::exists(path))return;auto record=parseJson(read(path,65536));auto phase=record.value("phase","");
 if(!record.is_object()||record.value("version",0)!=1||!record.contains("oldMarker")||!record["oldMarker"].is_string()||record["oldMarker"].get_ref<const std::string&>().size()>4096||(phase!="preparing"&&phase!="activating"&&phase!="result"&&phase!="sync"&&phase!="blocked"))throw std::runtime_error("Invalid persistent native update state");aide_host::Command::parse(record.at("command"));NativeMarker marker(record["oldMarker"]);
 if(phase=="blocked")throw std::runtime_error("Native update recovery is blocked until the owned process tree is inspected");
 if(phase=="preparing"||phase=="activating"){aide_host::Command::parse(record.at("command"));auto previous=record.at("oldMarker").get<std::string>();NativeMarker checked(previous);write(app.root/L".aide-image",previous+"\n");app.restore();}
}
