// Separate fault fixture. Production launcher contains no test switches.
#define wmain production_launcher_main
#include "../native/windows-launcher/main.cpp"
#undef wmain
#include <thread>
static void check(bool value,const char* message){if(!value)throw std::runtime_error(message);}
static int dockerFixture(int argc,wchar_t** argv){
 Args args;for(int i=1;i<argc;i++)args.push_back(argv[i]);auto mode=env(L"AIDE_FIXTURE_STOP_CASE");
 if(args.empty())return 90;
 if(args[0]==L"info"){if(mode==L"engine-off")return 1;std::cout<<json({{"OSType","linux"},{"Architecture","amd64"},{"MemTotal",uint64_t(8)*1024*1024*1024}}).dump();return 0;}
 if(args[0]==L"compose"){
  if(args.size()>1&&args[1]==L"version"){std::cout<<"Docker Compose fixture";return 0;}
  if(args.size()>1&&args[1]==L"config"){std::cout<<R"({"name":"aide-fixture","services":{"aide":{"volumes":[]}}})";return 0;}
  if(args.size()>1&&args[1]==L"stop")return mode==L"stop-fail"?1:0;
 }else if(args[0]==L"ps"){
  for(auto& arg:args)if(arg==L"label=com.docker.compose.project=aide-fixture")std::cout<<std::string(64,'a');
  return 0;
 }else if(args[0]==L"inspect"&&args.size()==4){
  json labels={{"com.docker.compose.project","aide-fixture"},{"com.docker.compose.project.working_dir",mode==L"foreign"?"C:/unrelated-owner":utf8(fs::current_path().wstring())}};
  if(args[3]==L"{{json .Config.Labels}}")std::cout<<labels.dump();
  else std::cout<<json({{"Config",{{"Labels",labels}}},{"State",{{"Running",mode==L"running"},{"Paused",false},{"Restarting",false},{"Pid",mode==L"running"?123:0},{"Status",mode==L"running"?"running":"exited"}}}}).dump();
  return 0;
 }
 std::cerr<<"Unsupported synthetic Docker command";return 91;
}
struct Child {
 Handle job,process;
 Child(DWORD milliseconds,DWORD code=0):job(CreateJobObjectW(nullptr,nullptr)){
  JOBOBJECT_EXTENDED_LIMIT_INFORMATION limits{};limits.BasicLimitInformation.LimitFlags=JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
  check(job.valid()&&SetInformationJobObject(job.h,JobObjectExtendedLimitInformation,&limits,sizeof(limits)),"fixture job");
  auto program=module();auto command=quote(program.wstring())+L" --sleep "+std::to_wstring(milliseconds)+L" "+std::to_wstring(code);STARTUPINFOW startup{};startup.cb=sizeof(startup);PROCESS_INFORMATION info{};
  check(CreateProcessW(program.c_str(),command.data(),nullptr,nullptr,FALSE,CREATE_SUSPENDED|CREATE_NO_WINDOW,nullptr,nullptr,&startup,&info),"fixture process");process.h=info.hProcess;Handle thread(info.hThread);check(AssignProcessToJobObject(job.h,process.h),"fixture assignment");check(ResumeThread(thread.h)!=DWORD(-1),"fixture resume");
 }
};
static void faultTests(const fs::path& root){
 stateRoot=root;fs::create_directories(root);auto target=root/L"state.json";write(target,"old");
 {Handle held(CreateFileW(target.c_str(),GENERIC_READ,FILE_SHARE_READ,nullptr,OPEN_EXISTING,0,nullptr));check(held.valid(),"hold target");std::thread release([&]{Sleep(150);CloseHandle(held.h);held.h=INVALID_HANDLE_VALUE;});try{write(target,"new");}catch(...){release.join();throw;}release.join();check(read(target)=="new","transient lock was not retried");}
 {Handle held(CreateFileW(target.c_str(),GENERIC_READ,FILE_SHARE_READ,nullptr,OPEN_EXISTING,0,nullptr));bool refused=false;auto began=GetTickCount64();try{write(target,"PRIVATE_SENTINEL");}catch(const std::exception& e){std::string message=e.what();refused=message.find("Windows error ")!=message.npos&&message.find("PRIVATE_SENTINEL")==message.npos;}check(refused&&GetTickCount64()-began<4000,"permanent lock must fail bounded with Windows error");check(read(target)=="new","failed commit damaged old state");}
 for(auto& item:fs::directory_iterator(root))check(item.path().filename().wstring().find(L".native-")==std::wstring::npos,"atomic temporary file leaked");
 std::atomic<bool> finished{false};std::exception_ptr readerError;
 std::thread reader([&]{try{while(!finished.load())check(read(target)=="new"||parseJson(read(target)).is_object(),"corrupt concurrent state");}catch(...){readerError=std::current_exception();}});
 try{for(int i=0;i<80;i++)write(target,json({{"value",i}}).dump());}catch(...){finished=true;reader.join();throw;}finished=true;reader.join();if(readerError)std::rethrow_exception(readerError);
 {Child child(1,259);check(WaitForSingleObject(child.process.h,5000)==WAIT_OBJECT_0,"child exit");terminateAndConfirm(child.job.h,{child.process.h},0);}
 {Child child(150);Handle query;check(DuplicateHandle(GetCurrentProcess(),child.job.h,GetCurrentProcess(),&query.h,JOB_OBJECT_QUERY,FALSE,0),"query handle");terminateAndConfirm(query.h,{child.process.h},0,2000);}
 {Child child(10000);Handle query;check(DuplicateHandle(GetCurrentProcess(),child.job.h,GetCurrentProcess(),&query.h,JOB_OBJECT_QUERY,FALSE,0),"query handle");bool refused=false;try{terminateAndConfirm(query.h,{child.process.h},0,100);}catch(const aide_host::Unsafe& e){refused=std::string(e.what()).find("Windows error 5")!=std::string::npos;}check(refused,"live process accepted after denied termination");terminateAndConfirm(child.job.h,{child.process.h},0);}
 {Child child(10000);terminateAndConfirm(child.job.h,{child.process.h},124);check(WaitForSingleObject(child.process.h,0)==WAIT_OBJECT_0,"timeout process survived");}
 {Child exited(1),live(10000);check(WaitForSingleObject(exited.process.h,5000)==WAIT_OBJECT_0,"exit fixture");Handle query;check(DuplicateHandle(GetCurrentProcess(),live.job.h,GetCurrentProcess(),&query.h,JOB_OBJECT_QUERY,FALSE,0),"query handle");bool refused=false;try{terminateAndConfirm(query.h,{exited.process.h},0,100);}catch(const aide_host::Unsafe&){refused=true;}check(refused,"signaled root hid active job descendants");terminateAndConfirm(live.job.h,{live.process.h},0);}
 {bool refused=false;try{terminateAndConfirm(INVALID_HANDLE_VALUE,{},0,1);}catch(const aide_host::Unsafe&){refused=true;}check(refused,"invalid job accepted");}
 auto result=run(module(),{L"--sleep",L"10000",L"0"},50);check(result.code==124,"run timeout did not return 124");
 write(root/L"fault-results.json",json({{"atomicSharingRetry",true},{"atomicFailurePreservesState",true},{"concurrentStateReaders",true},{"naturalExitRace",true},{"liveProcessRefused",true},{"descendantsChecked",true},{"invalidJobRefused",true},{"timeoutConfirmed",true}}).dump(2));
}
int wmain(int argc,wchar_t** argv){
 try{if(module().filename()==L"docker.exe")return dockerFixture(argc,argv);if(argc>2&&std::wstring(argv[1])==L"--sleep"){Sleep(DWORD(std::stoul(argv[2])));return argc>3?int(std::stoul(argv[3])):0;}if(argc!=2)return 2;faultTests(argv[1]);return 0;}catch(const std::exception& e){std::cerr<<e.what()<<'\n';return 1;}
}
