#pragma once
// Native user-session workers. All mutating calls acquire the installation
// lifecycle lock; independent worker processes share this executable, not PS1.
struct NativeLock {
 Handle file;
 explicit NativeLock(const fs::path& path):file([&]{realPath(path);fs::create_directories(path.parent_path());return CreateFileW(path.c_str(),GENERIC_READ|GENERIC_WRITE,0,nullptr,OPEN_ALWAYS,FILE_ATTRIBUTE_NORMAL|FILE_FLAG_OPEN_REPARSE_POINT,nullptr);}()){
  if(!file.valid()&&GetLastError()!=ERROR_SHARING_VIOLATION&&GetLastError()!=ERROR_LOCK_VIOLATION)throw std::runtime_error("Cannot acquire native task lock");
  if(file.valid()){BY_HANDLE_FILE_INFORMATION info{};if(!GetFileInformationByHandle(file.h,&info)||(info.dwFileAttributes&FILE_ATTRIBUTE_REPARSE_POINT))throw std::runtime_error("Native task lock is unreadable or a reparse point");}
 }
 bool owns()const{return file.valid();}
};
struct NativeMarker {
 std::string reference,id,platform,tag;
 explicit NativeMarker(const std::string& text){std::istringstream in(text);std::string extra;in>>reference>>id>>platform;in>>tag;if(in>>extra)throw std::runtime_error("Invalid installed image marker");
  if(!std::regex_match(reference,std::regex("[A-Za-z0-9][A-Za-z0-9._:/-]*"))||!imageId(id)||(platform!="linux/amd64"&&platform!="linux/arm64")||(!tag.empty()&&!std::regex_match(tag,std::regex("v[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+-RC[0-9]+"))))throw std::runtime_error("Invalid installed image marker identity");
 }
};
struct NativeStage {
 fs::path path;
 explicit NativeStage(const fs::path& root){path=root/(L"stage-"+std::to_wstring(GetCurrentProcessId())+L"-"+std::to_wstring(GetTickCount64()));if(!fs::create_directory(path))throw std::runtime_error("Cannot create owned native stage");}
 ~NativeStage(){std::error_code error;fs::remove_all(path,error);}
};
class NativeUpdateHost {
 Launcher& app;fs::path directory,journal;std::string token;
 void context(){
  int port=app.mappedPort();if(port<1||port>65535||app.container().empty())throw std::runtime_error("Current installation is not running");
  auto path=app.root/L".aide-data/storage/data/auth/access-token";realPath(path);
  if(fs::exists(path))token=trim(read(path,4096));else{auto result=app.compose({L"exec",L"-T",L"aide",L"cat",L"/data/auth/access-token"});if(result.code)result=app.compose({L"exec",L"-T",L"aide",L"cat",L"/data/access-token"});token=result.code?"":trim(result.out);}
  if(token.size()<32||token.size()>4096)throw std::runtime_error("Private update authentication is unavailable");
  base="https://127.0.0.1:"+std::to_string(port)+"/api/updates/agent";
 }
 std::string base;
 aide_http::Response api(const std::string& path,const std::string& method="GET",const json& body=nullptr,const std::function<void(const char*,size_t)>& sink={}){
  context();return aide_http::request(base+path,method,token,body.is_null()?"":body.dump(),sink,sink?(uint64_t(2)<<30)+1048576:65536,true);
 }
 void checkRunning(const std::string& id){auto cid=app.container();if(!digest(cid))throw std::runtime_error("Target container is not uniquely running");auto running=app.d({L"inspect",wide(cid),L"--format",L"{{.Image}}"});app.required(running,"Cannot inspect activated image");if(trim(running.out)!=id||!app.healthy())throw std::runtime_error("Running image identity or health differs from the requested slot");}
public:
 explicit NativeUpdateHost(Launcher& value):app(value),directory(stateRoot/L"update"),journal(directory/L"transaction.json"){
  privateDirectory(directory);realPath(journal);
 }
 std::string redact(const std::string& text){return aide_host::diagnostic(text,token);}
 void log(const std::string& event,const std::string& message){
  auto path=directory/L"agent.log",previous=directory/L"agent.log.1";realPath(path);realPath(previous);
  if(fs::exists(path)&&fs::file_size(path)>=1024*1024){fs::remove(previous);fs::rename(path,previous);}
  SYSTEMTIME now{};GetSystemTime(&now);char stamp[64];snprintf(stamp,sizeof(stamp),"%04u-%02u-%02uT%02u:%02u:%02uZ",now.wYear,now.wMonth,now.wDay,now.wHour,now.wMinute,now.wSecond);
  std::ofstream file(path,std::ios::app);if(!(file<<stamp<<' '<<event<<' '<<redact(message)<<'\n'))throw std::runtime_error("Cannot save native agent diagnostic");
 }
 json load(){realPath(journal);return fs::exists(journal)?parseJson(read(journal,65536)):json();}
 void store(const json& value){write(journal,value.dump());}
 void clear(){auto previous=load();fs::remove(journal);log("transaction-complete",previous.value("phase","unknown"));}
 std::string marker(){realPath(app.root/L".aide-image");auto value=trim(read(app.root/L".aide-image",4096));NativeMarker checked(value);return value;}
 std::string command(){auto result=api("");if(result.status==204)return {};if(result.status!=200)throw std::runtime_error("Native update poll was refused");return trim(result.body);}
 void progress(const aide_host::Command& c,const char* phase,int percent,const char* message){try{api("/progress","POST",{{"operationId",c.operation},{"status",phase},{"progress",percent},{"message",message}});}catch(const std::exception&){log("progress-retry",phase);}}
 std::string prepare(const aide_host::Command& c){
  if(c.platform!=app.platform)throw std::runtime_error("Requested slot platform differs from the current Docker engine");
  progress(c,"preparing",5,"Preparing native slot activation");auto old=NativeMarker(marker());
  app.rollbackTag(old.reference,c.image);app.rollbackTag(c.reference,c.image);
  json metadata;
  if(!c.package.empty()){
   NativeStage stage(directory);auto zip=stage.path/L"package.zip",archive=stage.path/L"image.tar.gz";
   progress(c,"downloading",15,"Downloading the staged package");
   {Handle file(CreateFileW(zip.c_str(),GENERIC_WRITE,0,nullptr,CREATE_NEW,FILE_ATTRIBUTE_NORMAL,nullptr));if(!file.valid())throw std::runtime_error("Cannot create native package destination");
    auto response=api("/packages/"+c.package,"GET",nullptr,[&](const char* data,size_t size){DWORD count=0;if(!WriteFile(file.h,data,DWORD(size),&count,nullptr)||count!=size)throw std::runtime_error("Cannot save staged package");});if(response.status!=200)throw std::runtime_error("Staged package download was refused");}
   auto current=app.d({L"inspect",wide(app.container()),L"--format",L"{{.Image}}"});app.required(current,"Cannot identify current extraction runtime");auto runtimeId=trim(current.out);if(!imageId(runtimeId))throw std::runtime_error("Invalid extraction runtime identity");
   auto name="aide-"+c.tag+"-linux-"+c.platform.substr(6)+"-image.tar.gz";
   app.required(app.ownedRun({L"run",L"--rm",L"--pull",L"never",L"--network",L"none",L"--user",L"0:0",L"--memory",L"256m",L"--cpus",L"1",L"-v",stage.path.wstring()+L":/stage",L"--entrypoint",L"python3",wide(runtimeId),L"-c",wide(embedded_host_data),L"extract-update",L"/stage/package.zip",L"/stage/image.tar.gz",wide(name)},1200000),"Fixed image member extraction failed");
   progress(c,"verifying",38,"Verifying the staged image SHA256");if(sha256(archive)!=c.hash)throw std::runtime_error("Staged image SHA256 mismatch");
   progress(c,"importing",52,"Importing the verified image");auto loaded=app.d({L"image",L"load",L"-i",archive.wstring()},900000);app.required(loaded,"Verified image import failed");metadata=app.inspect(c.image);if(metadata.is_null())metadata=app.inspect(aide_image::receipt(loaded.out));
  }else{metadata=app.inspect(c.reference);if(metadata.is_null()||metadata.value("Id","")!=c.image)throw std::runtime_error("Requested local slot image is unavailable");}
  if(!runtime(metadata,c.platform))throw std::runtime_error("Imported slot runtime/platform/source labels are invalid");auto canonical=metadata.value("Id","");
  app.required(app.d({L"image",L"tag",wide(canonical),wide(c.reference)}),"Cannot tag verified target slot");return canonical;
 }
 void activate(const aide_host::Command& c,const std::string& canonical){
  progress(c,"activating",70,"Activating native target slot");write(app.root/L".aide-image",c.reference+" "+canonical+" "+c.platform+" "+c.tag+"\n");
  if(fs::exists(stateRoot/L"stop"))throw std::runtime_error("Activation stopped by operator");
  Launcher target(app.root);target.noBrowser=true;target.allowDesktop=false;target.restore();target.start();
 }
 void verify(const aide_host::Command& c,const std::string& canonical){progress(c,"health-check",88,"Checking target identity and health");checkRunning(canonical);}
 void rollback(const std::string& previous){
  NativeMarker old(previous);write(app.root/L".aide-image",previous+"\n");
  if(fs::exists(stateRoot/L"stop"))throw std::runtime_error("Operator stop retained the previous marker; restart is paused");
  app.required(app.d({L"image",L"tag",wide(old.id),wide(old.reference)}),"Cannot restore previous slot reference");
  Launcher target(app.root);target.noBrowser=true;target.allowDesktop=false;target.restore();target.start();checkRunning(old.id);
 }
 aide_host::Ack submit(const json& body){
  auto response=api("/result","POST",body);if(response.status>=200&&response.status<300)return aide_host::Ack::accepted;
  if(response.status==409){auto pending=api("");if(pending.status==204)return aide_host::Ack::obsolete;if(pending.status==200&&!trim(pending.body).empty()&&aide_host::Command::tsv(trim(pending.body)).operation!=body.value("operationId",""))return aide_host::Ack::obsolete;}
  log("result-retry",body.value("operationId",""));return aide_host::Ack::retry;
 }
 bool sync(const aide_host::Command& c,const std::string& canonical){
  auto response=api("/sync","POST",{{"version",1},{"activeSlot",c.target},{"slots",{{c.target,{{"version",c.version},{"tag",c.tag},{"imageRef",c.reference},{"imageId",canonical},{"platform",c.platform}}}}}});return response.status>=200&&response.status<300;
 }
 bool legacyResult(){
  auto blocker=app.root/L".aide-data/update-agent/launcher-stop-required",pending=app.root/L".aide-data/update-agent/pending-result.json";realPath(blocker);realPath(pending);
  if(fs::exists(blocker))throw aide_host::Unsafe("Previous launcher termination needs inspection; legacy blocker retained");
  if(!fs::exists(pending))return true;auto value=parseJson(read(pending,16384));
  if(!value.is_object()||!value.contains("result")||!value["result"].is_object()||!std::regex_match(value["result"].value("operationId",""),std::regex("[a-f0-9]{32}"))||!value["result"].contains("success")||!value["result"]["success"].is_boolean()||!value["result"].contains("message")||!value["result"]["message"].is_string())throw std::runtime_error("Invalid legacy pending result");
  value["result"]["message"]=redact(value["result"]["message"]);
  if(!value.value("nativeAcknowledged",false)){
   auto ack=submit(value["result"]);if(ack==aide_host::Ack::retry)return false;if(ack==aide_host::Ack::obsolete){fs::remove(pending);return true;}value["nativeAcknowledged"]=true;write(pending,value.dump());
  }
  if(value.contains("canonical")&&!value["canonical"].is_null()){
   auto response=api("/sync","POST",value["canonical"]);if(response.status<200||response.status>=300)return false;
  }
  fs::remove(pending);return true;
 }
 bool seed(){
  context();auto response=aide_http::request(base.substr(0,base.size()-6)+"/slots","GET",token,"",{},65536,true);if(response.status!=200)throw std::runtime_error("Cannot read native slot runtime mode");auto state=parseJson(response.body);if(state.value("runtimeMode","")!="release-image")return false;
  auto installed=app.root/L".aide-image";
  if(!fs::exists(installed)){auto current=app.d({L"inspect",wide(app.container()),L"--format",L"{{.Image}}"});app.required(current,"Cannot identify native bootstrap image");auto id=trim(current.out);if(!imageId(id))throw std::runtime_error("Invalid bootstrap image identity");write(installed,"aide:slot-a "+id+" "+app.platform+"\n");app.save();}
  auto old=NativeMarker(marker());checkRunning(old.id);auto slot=old.reference=="aide:slot-b"?"B":"A";auto reference=std::string("aide:slot-")+(slot[0]=='A'?'a':'b');app.required(app.d({L"image",L"tag",wide(old.id),wide(reference)}),"Cannot retain active slot identity");
  auto version=old.tag.empty()?"":old.tag.substr(1);auto index=version.find("-RC");if(index!=version.npos)version.replace(index,3," RC");
  if(old.tag.empty()&&state.contains("slots")&&state["slots"].is_object()&&state["slots"].contains(slot)){auto previous=state["slots"][slot];auto priorId=previous.value("imageId","");if(priorId.empty()||priorId==old.id){auto tag=previous.value("tag","");if(std::regex_match(tag,std::regex("v[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+-RC[0-9]+")))old.tag=tag;version=previous.value("version","");}}
  response=api("/sync","POST",{{"version",1},{"activeSlot",slot},{"slots",{{slot,{{"version",version},{"tag",old.tag},{"imageRef",reference},{"imageId",old.id},{"platform",old.platform}}}}}});if(response.status<200||response.status>=300)throw std::runtime_error("Cannot seed current native slot state");
  if(!old.tag.empty()){write(installed,old.reference+" "+old.id+" "+old.platform+" "+old.tag+"\n");app.save();}return true;
 }
};
