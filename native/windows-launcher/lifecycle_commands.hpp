// Included in Launcher: native implementations of the #640 standard commands.
// Compilers and Python run inside Docker build stages, never on Windows hosts.
 bool sourcePackage()const{return fs::exists(root/L".git")||(fs::is_regular_file(root/L"go.mod")&&fs::is_regular_file(root/L"Dockerfile"));}
 void requireSource(){if(!sourcePackage())throw std::runtime_error("Release packages provide start, stop, docker_clean and verify; use a Git source package for build, clean or release");}
 std::pair<std::string,std::string> versionIdentity(){
  std::smatch match;if(fs::exists(root/L"version.md")){auto text=read(root/L"version.md");if(std::regex_search(text,match,std::regex("([0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+) RC([1-9][0-9]*)")))return {match[0].str(),"v"+match[1].str()+"-RC"+match[2].str()};}
  if(fs::exists(root/L".aide-image")){std::istringstream marker(read(root/L".aide-image"));std::string ref,id,osarch,tag;marker>>ref>>id>>osarch>>tag;if(std::regex_match(tag,std::regex("v[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+-RC[1-9][0-9]*")))return {tag.substr(1),tag};}
  throw std::runtime_error("Version identity missing; restore version.md or the complete Release package");
 }
 std::string buildScope(){auto file=stateRoot/L"build-root";write(file,nativeRoot());return sha256(file).substr(0,20);}
 json buildOwner(const std::string& scope){auto dir=root/L".aide-data/build";realPath(dir);auto file=dir/L"owner.json";realPath(file);if(!fs::exists(file))return {};auto value=parseJson(read(file,4096));if(value!=json{{"version",1},{"scope",scope},{"builder","aide-"+scope}})throw std::runtime_error("Build ownership receipt does not match this installation");return value;}
 void ensureBuilder(const std::string& scope){
  auto builder="aide-"+scope;auto found=d({L"buildx",L"inspect",wide(builder)});
  if(!found.code){if(buildOwner(scope).is_null())throw std::runtime_error("Builder exists without this installation receipt; refusing to adopt it");return;}
  required(d({L"buildx",L"create",L"--name",wide(builder),L"--driver",L"docker-container",L"--driver-opt",L"memory=4g,memory-swap=4g,cpu-period=100000,cpu-quota=200000,restart-policy=no"}),"Cannot create this installation build environment");
  privateDirectory(root/L".aide-data/build");write(root/L".aide-data/build/owner.json",json{{"version",1},{"scope",scope},{"builder",builder}}.dump());
 }
 fs::path buildSource(){
  requireSource();disk(true);engine(allowDesktop);auto scope=buildScope();ensureBuilder(scope);auto builder="aide-"+scope,ref="aide:source-"+scope;
  auto identity=versionIdentity();auto git=findExe(L"git.exe");if(git.empty())throw std::runtime_error("Git is required for a source build");auto revision=run(git,{L"-C",root.wstring(),L"rev-parse",L"HEAD"});required(revision,"Cannot identify source commit");auto commit=trim(revision.out);if(!std::regex_match(commit,std::regex("[a-f0-9]{40}")))throw std::runtime_error("Invalid source commit");auto dirty=run(git,{L"-C",root.wstring(),L"status",L"--porcelain"});required(dirty,"Cannot inspect source changes");if(!trim(dirty.out).empty())commit+="-dirty";
  auto fingerprint=stateRoot/L"source-input.txt";std::string hashes;for(const auto& name:aide_source::inputs(root,parseJson(embedded_source_manifest)))hashes+=sha256(root/fs::u8path(name))+"  "+name+"\n";write(fingerprint,hashes);auto source=sha256(fingerprint);
  Args arguments={L"buildx",L"build",L"--builder",wide(builder),L"--load",L"--pull=false",L"--label",wide("aide.installation="+scope),L"--label",L"aide.build-role=runtime",L"--tag",wide(ref)};
  auto configuredBuild=config()["services"]["aide"]["build"];auto file=configuredBuild.value("dockerfile","Dockerfile");arguments.insert(arguments.end(),{L"--file",wide(file)});
  if(configuredBuild.contains("network"))arguments.insert(arguments.end(),{L"--network",wide(configuredBuild["network"].get<std::string>())});
  auto values=configuredBuild.value("args",json::object());values["AIDE_VERSION"]=identity.first;values["AIDE_COMMIT"]=commit;values["AIDE_SRC_SHA"]=source;values["AIDE_RUNTIME_MODE"]="source";
  for(auto item=values.begin();item!=values.end();++item)if(!item.value().is_null())arguments.insert(arguments.end(),{L"--build-arg",wide(item.key()+"="+(item.value().is_string()?item.value().get<std::string>():item.value().dump()))});
  if(force)arguments.push_back(L"--no-cache");arguments.push_back(L".");say("Building current source and embedded runtime components in the owned builder...");required(d(arguments,7200000),"Source build failed; no Release substitution was performed");
  auto built=inspect(ref);if(!runtime(built,platform)||label(built,"aide.srcsha")!=source||label(built,"aide.installation")!=scope)throw std::runtime_error("Built source image identity/fingerprint/installation did not verify");
  auto hostRef="aide:native-"+scope;Args native={L"buildx",L"build",L"--builder",wide(builder),L"--load",L"--pull=false",L"--target",L"native-launcher",L"--file",L"Dockerfile",L"--label",wide("aide.installation="+scope),L"--label",L"aide.build-role=native-launcher",L"--build-arg",wide("AIDE_LAUNCHER_ARCH="+hostArch()),L"--tag",wide(hostRef),L"."};
  say("Building the current native Windows host inside Docker...");required(d(native,7200000),"Native host build failed; existing startup configuration retained");
  auto created=d({L"create",L"--network",L"none",L"--entrypoint",L"/bin/true",wide(hostRef)});required(created,"Cannot extract compiled native host");auto cid=trim(created.out);if(!digest(cid))throw std::runtime_error("Invalid extraction container identity");
  auto dir=stateRoot/L"compiled";privateDirectory(dir);auto temporary=dir/(L"extract-"+std::to_wstring(GetCurrentProcessId())+L".exe");realPath(temporary);
  try{required(d({L"cp",wide(cid+":/out/aide-launcher.exe"),temporary.wstring()}),"Cannot copy compiled native host");required(d({L"rm",wide(cid)}),"Cannot remove owned extraction container");}catch(...){d({L"rm",wide(cid)});fs::remove(temporary);throw;}
  auto bytes=read(temporary,16*1024*1024);if(bytes.size()<64||bytes.substr(0,2)!="MZ")throw std::runtime_error("Compiled native host is not a PE executable");auto hash=sha256(temporary);auto destination=dir/wide("launcher-"+hash+".exe");realPath(destination);if(fs::exists(destination)){if(sha256(destination)!=hash)throw std::runtime_error("Compiled host cache mismatch");fs::remove(temporary);}else fs::rename(temporary,destination);
  write(stateRoot/L"compiled-launcher.json",json{{"version",1},{"file",utf8(destination.filename().wstring())},{"sha256",hash},{"source",source}}.dump());
  setting(root,"AIDE_IMAGE",ref);setting(root,"AIDE_INSTALLATION_KIND","source");settings["AIDE_IMAGE"]=ref;setenvw(L"AIDE_IMAGE",wide(ref));setenvw(L"AIDE_INSTALLATION_KIND",L"source");image=built.value("Id","");say("Source and native host verified; starting the compiled installation.");return destination;
 }
 void dockerClean(){
  engine(false);auto scope=buildScope();auto owner=buildOwner(scope);auto listed=compose({L"ps",L"-aq"});required(listed,"Cannot inspect this installation containers");std::set<std::string> images;std::istringstream containers(listed.out);std::string cid;
  while(containers>>cid){if(!std::regex_match(cid,std::regex("[a-f0-9]{12,64}")))throw std::runtime_error("Invalid owned container identity");auto current=d({L"inspect",wide(cid),L"--format",L"{{json .}}"});required(current,"Cannot verify cleanup ownership");auto value=parseJson(current.out);if(!aide_installation::owns(root,label(value,"com.docker.compose.project.working_dir")))throw std::runtime_error("Foreign installation in cleanup project");auto id=value.value("Image","");if(!imageId(id))throw std::runtime_error("Invalid cleanup image identity");images.insert(id);}
  auto owned=d({L"image",L"ls",L"-q",L"--no-trunc",L"--filter",wide("label=aide.installation="+scope)});required(owned,"Cannot list owned build outputs");std::istringstream more(owned.out);std::string id;while(more>>id){if(!imageId(id))throw std::runtime_error("Invalid build output identity");images.insert(id);}
  if(!stop())throw std::runtime_error("Application stop was not confirmed; cleanup refused");required(compose({L"down",L"--remove-orphans"},180000),"Cannot remove this installation containers/network");
  for(const auto& id:images){auto users=d({L"ps",L"-aq",L"--filter",wide("ancestor="+id)});required(users,"Cannot check shared image users");if(!trim(users.out).empty()){say("Shared image retained: "+id);continue;}if(d({L"image",L"rm",wide(id)}).code)say("Image has other references and was retained: "+id);}
  if(!owner.is_null()){auto builder="aide-"+scope;if(!d({L"buildx",L"inspect",wide(builder)}).code)required(d({L"buildx",L"rm",wide(builder)},180000),"Cannot remove owned build cache");fs::remove(root/L".aide-data/build/owner.json");}
  say("Owned Docker environment removed; persistent data and offline packages retained.");
 }
 int verify(){
  fs::current_path(root);
  json result={{"package",sourcePackage()?"source":"release"},{"checks",json::object()}};bool ok=true;try{auto v=versionIdentity();result["version"]=v.first;result["tag"]=v.second;}catch(const std::exception& e){result["checks"]["version"]=e.what();ok=false;}
  docker=findExe(L"docker.exe");if(docker.empty())docker=fs::path(env(L"ProgramFiles"))/L"Docker/Docker/resources/bin/docker.exe";
  if(!fs::exists(docker)){result["checks"]["docker"]="unavailable";ok=false;}else{
   auto engine=d({L"info"},15000);result["checks"]["docker"]=engine.code?"unavailable":"pass";ok=ok&&!engine.code;auto composer=compose({L"version"});result["checks"]["compose"]=composer.code?"unavailable":"pass";ok=ok&&!composer.code;
   auto configured=compose({L"config",L"--quiet"});result["checks"]["configuration"]=configured.code?"invalid":"pass";ok=ok&&!configured.code;bool ready=!configured.code&&healthy();result["checks"]["runtime"]=ready?"pass":"not running or unhealthy";ok=ok&&ready;
   if(sourcePackage()){auto git=findExe(L"git.exe");bool hasGit=!git.empty()&&!run(git,{L"--version"}).code;result["checks"]["git"]=hasGit?"pass":"unavailable";auto buildx=d({L"buildx",L"version"});result["checks"]["buildx"]=buildx.code?"unavailable":"pass";ok=ok&&hasGit&&!buildx.code;}
  }result["startupLog"]=".aide-data/native/launcher.log";std::cout<<result.dump(2)<<'\n';return ok?0:1;
 }
 void release(){
  requireSource();auto identity=versionIdentity();auto git=findExe(L"git.exe"),gh=findExe(L"gh.exe");if(git.empty()||gh.empty())throw std::runtime_error("release requires native Git and GitHub CLI");
  auto capture=[&](Args args){auto r=run(git,args);required(r,"Cannot verify release Git identity");return trim(r.out);};
  if(capture({L"-C",root.wstring(),L"branch",L"--show-current"})!="main"||!capture({L"-C",root.wstring(),L"status",L"--porcelain"}).empty())throw std::runtime_error("release requires clean main; follow the existing version/tag workflow first");
  auto head=capture({L"-C",root.wstring(),L"rev-parse",L"HEAD"});if(capture({L"-C",root.wstring(),L"rev-parse",wide("refs/tags/"+identity.second+"^{}")})!=head)throw std::runtime_error("Release tag must identify this main commit");
  auto notes=root/L"docs/releases"/wide(identity.second+".md");auto text=read(notes);if(text.rfind("# aide "+identity.second,0)!=0||text.size()<150||text.find("TODO")!=text.npos)throw std::runtime_error("Reviewed release notes for this exact tag are required");for(auto title:{"新增与改进","修复","升级影响","已知问题"})if(text.find(std::string("## ")+title)==text.npos)throw std::runtime_error("Release notes are incomplete");
  auto refs=capture({L"-C",root.wstring(),L"ls-remote",L"origin",L"refs/heads/main",wide("refs/tags/"+identity.second+"^{}")});std::map<std::string,std::string> remote;std::istringstream rows(refs);std::string sha,name;while(rows>>sha>>name)remote[name]=sha;if(remote["refs/heads/main"]!=head||remote["refs/tags/"+identity.second+"^{}"]!=head)throw std::runtime_error("Remote main/tag must match this release commit");
  auto checks=run(gh,{L"api",wide("repos/skyelan1999/aide/commits/"+head+"/check-runs")});required(checks,"Cannot verify current main gate");auto checked=parseJson(checks.out);bool passed=false;for(auto& c:checked["check_runs"])if(c.value("name","")=="gate"&&c.value("status","")=="completed"&&c.value("conclusion","")=="success")passed=true;if(!passed)throw std::runtime_error("Current main has no successful required gate; release not dispatched");
  required(run(gh,{L"workflow",L"run",L"aide-delivery.yml",L"--repo",L"skyelan1999/aide",L"--ref",L"main",L"-f",wide("release_tag="+identity.second)}),"Release delivery dispatch failed");say("Delivery dispatched for "+identity.second+"; native builds, notes, cadence and environment approval remain enforced. Dispatch is not publication.");
 }
