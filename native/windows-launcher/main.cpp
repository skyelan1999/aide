// Native Windows lifecycle entry. No shell, PowerShell, or host Python runtime.
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <winsock2.h>
#include <windows.h>
#include <shellapi.h>
#include <shobjidl.h>
#include <sddl.h>
#include <aclapi.h>
#include <bcrypt.h>
#include <ws2tcpip.h>
#include <filesystem>
#include <fstream>
#include <sstream>
#include <iostream>
#include <vector>
#include <map>
#include <regex>
#include <algorithm>
#include <chrono>
#include <thread>
#include <memory>
#include <stdexcept>
#include "third_party/json.hpp"
#include "embedded.hpp"
#include "archive.hpp"
#include "image.hpp"
#include "source_inputs.hpp"
#include "installation.hpp"
#include "host_state.hpp"
using json=nlohmann::json;
namespace fs=std::filesystem;
using Args=std::vector<std::wstring>;
struct Handle {
 HANDLE h=INVALID_HANDLE_VALUE;
 Handle()=default; explicit Handle(HANDLE v):h(v){}
 ~Handle(){if(h && h!=INVALID_HANDLE_VALUE)CloseHandle(h);}
 Handle(const Handle&)=delete; Handle& operator=(const Handle&)=delete;
 bool valid()const{return h && h!=INVALID_HANDLE_VALUE;}
};
std::wstring wide(const std::string& s){
 if(s.empty())return {};
 int n=MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,s.data(),int(s.size()),nullptr,0);
 if(!n)throw std::runtime_error("Invalid UTF-8 path or argument");
 std::wstring r(n,0);MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,s.data(),int(s.size()),r.data(),n);return r;
}
std::string utf8(const std::wstring& s){
 if(s.empty())return {};
 int n=WideCharToMultiByte(CP_UTF8,WC_ERR_INVALID_CHARS,s.data(),int(s.size()),nullptr,0,nullptr,nullptr);
 if(!n)throw std::runtime_error("Invalid Unicode path");
 std::string r(n,0);WideCharToMultiByte(CP_UTF8,WC_ERR_INVALID_CHARS,s.data(),int(s.size()),r.data(),n,nullptr,nullptr);return r;
}
std::string trim(std::string s){auto b=s.find_first_not_of(" \r\n\t");return b==s.npos?"":s.substr(b,s.find_last_not_of(" \r\n\t")-b+1);}
json parseJson(const std::string& text){try{return json::parse(text);}catch(const json::exception&){throw std::runtime_error("Invalid JSON metadata; contents omitted to protect private configuration");}}
std::wstring env(const wchar_t* key){DWORD n=GetEnvironmentVariableW(key,nullptr,0);if(!n)return {};std::wstring v(n,0);GetEnvironmentVariableW(key,v.data(),n);v.resize(n-1);return v;}
void setenvw(const wchar_t* k,const std::wstring& v){if(!SetEnvironmentVariableW(k,v.c_str()))throw std::runtime_error("Cannot set launcher process environment");}
std::string read(const fs::path& p,size_t max=32*1024*1024){
 // Readers allow atomic rename of live status/journal files. CRT ifstream
 // sharing semantics can otherwise make a concurrent status read kill a writer.
 Handle file(CreateFileW(p.c_str(),GENERIC_READ,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,nullptr,OPEN_EXISTING,FILE_ATTRIBUTE_NORMAL,nullptr));if(!file.valid())throw std::runtime_error("Cannot read required launcher file");
 std::string s;char buffer[65536];while(true){DWORD n=0;if(!ReadFile(file.h,buffer,sizeof(buffer),&n,nullptr))throw std::runtime_error("Cannot read launcher metadata");if(!n)break;if(size_t(n)>max-s.size())throw std::runtime_error("Launcher metadata exceeds size limit");s.append(buffer,n);}
 if(s.compare(0,3,"\xef\xbb\xbf")==0)s.erase(0,3);return s;
}
void realPath(const fs::path& p){DWORD a=GetFileAttributesW(p.c_str());if(a!=INVALID_FILE_ATTRIBUTES && (a&FILE_ATTRIBUTE_REPARSE_POINT))throw std::runtime_error("Persistent launcher paths cannot be links or junctions");}
void write(const fs::path& p,const std::string& s){
 static unsigned serial=0;realPath(p);fs::create_directories(p.parent_path());auto t=p;t+=L".native-"+std::to_wstring(GetCurrentProcessId())+L"-"+std::to_wstring(GetTickCount64())+L"-"+std::to_wstring(++serial);realPath(t);
 struct Cleanup{fs::path p;~Cleanup(){std::error_code error;fs::remove(p,error);}}cleanup{t};
 {Handle file(CreateFileW(t.c_str(),GENERIC_WRITE,0,nullptr,CREATE_NEW,FILE_ATTRIBUTE_NORMAL,nullptr));if(!file.valid())throw std::runtime_error("Cannot create atomic launcher state");for(size_t offset=0;offset<s.size();){DWORD written=0,n=DWORD(std::min<size_t>(65536,s.size()-offset));if(!WriteFile(file.h,s.data()+offset,n,&written,nullptr)||written!=n)throw std::runtime_error("Cannot save launcher state");offset+=n;}if(!FlushFileBuffers(file.h))throw std::runtime_error("Cannot flush launcher state before commit");}
 if(!MoveFileExW(t.c_str(),p.c_str(),MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH))throw std::runtime_error("Cannot commit launcher state");
}
fs::path module(){std::wstring s(32768,0);DWORD n=GetModuleFileNameW(nullptr,s.data(),DWORD(s.size()));if(!n||n>=s.size())throw std::runtime_error("Cannot locate launcher executable");s.resize(n);return fs::path(s);}
std::wstring quote(const std::wstring& s){
 std::wstring r=L"\"";size_t slashes=0;
 for(wchar_t c:s){if(c==L'\\'){slashes++;continue;}if(c==L'\"'){r.append(slashes*2+1,L'\\');r+=c;}else{r.append(slashes,L'\\');r+=c;}slashes=0;}
 r.append(slashes*2,L'\\');return r+L'\"';
}
struct Result{DWORD code;std::string out,err;};
struct Startup {
 STARTUPINFOEXW info{};std::vector<BYTE> storage;std::vector<HANDLE> handles;
 Startup(HANDLE input,HANDLE output,HANDLE error){
  info.StartupInfo.cb=sizeof(info);info.StartupInfo.dwFlags=STARTF_USESTDHANDLES;
  info.StartupInfo.hStdInput=input;info.StartupInfo.hStdOutput=output;info.StartupInfo.hStdError=error;
  handles={input,output};if(error!=output)handles.push_back(error);
  SIZE_T size=0;InitializeProcThreadAttributeList(nullptr,1,0,&size);storage.resize(size);info.lpAttributeList=reinterpret_cast<LPPROC_THREAD_ATTRIBUTE_LIST>(storage.data());
  if(!InitializeProcThreadAttributeList(info.lpAttributeList,1,0,&size)||!UpdateProcThreadAttribute(info.lpAttributeList,0,PROC_THREAD_ATTRIBUTE_HANDLE_LIST,handles.data(),handles.size()*sizeof(HANDLE),nullptr,nullptr))throw std::runtime_error("Cannot restrict child process handle inheritance");
 }
 ~Startup(){DeleteProcThreadAttributeList(info.lpAttributeList);}
};
fs::path stateRoot;
fs::path nativeStopPath;
bool observeNativeStop=false;
bool nativeInterrupted(){return observeNativeStop&&!nativeStopPath.empty()&&GetFileAttributesW(nativeStopPath.c_str())!=INVALID_FILE_ATTRIBUTES;}
unsigned sequence=0;
Result run(const fs::path& exe,const Args& args,DWORD timeout=120000){
 auto stem=L"process-"+std::to_wstring(GetCurrentProcessId())+L"-"+std::to_wstring(GetTickCount64())+L"-"+std::to_wstring(++sequence);
 auto out=stateRoot/(stem+L".out"),err=stateRoot/(stem+L".err");
 struct Cleanup{fs::path a,b;~Cleanup(){std::error_code error;fs::remove(a,error);fs::remove(b,error);}}cleanup{out,err};
 SECURITY_ATTRIBUTES sa{sizeof(sa),nullptr,TRUE};
 Handle o(CreateFileW(out.c_str(),GENERIC_WRITE,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,&sa,CREATE_NEW,FILE_ATTRIBUTE_TEMPORARY,nullptr));
 Handle e(CreateFileW(err.c_str(),GENERIC_WRITE,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,&sa,CREATE_NEW,FILE_ATTRIBUTE_TEMPORARY,nullptr));
 Handle input(CreateFileW(L"NUL",GENERIC_READ,FILE_SHARE_READ|FILE_SHARE_WRITE,&sa,OPEN_EXISTING,0,nullptr));
 if(!o.valid()||!e.valid()||!input.valid())throw std::runtime_error("Cannot capture native process output");
 std::wstring cmd=quote(exe.wstring());for(auto& a:args)cmd+=L" "+quote(a);
 Startup si(input.h,o.h,e.h);
 Handle job(CreateJobObjectW(nullptr,nullptr));JOBOBJECT_EXTENDED_LIMIT_INFORMATION limits{};limits.BasicLimitInformation.LimitFlags=JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
 if(!job.valid()||!SetInformationJobObject(job.h,JobObjectExtendedLimitInformation,&limits,sizeof(limits)))throw std::runtime_error("Cannot bound native process lifetime");
 PROCESS_INFORMATION pi{};
 if(!CreateProcessW(exe.c_str(),cmd.data(),nullptr,nullptr,TRUE,CREATE_SUSPENDED|CREATE_NO_WINDOW|EXTENDED_STARTUPINFO_PRESENT,nullptr,nullptr,&si.info.StartupInfo,&pi))throw std::runtime_error("Cannot execute required native program (Windows error "+std::to_string(GetLastError())+")");
 Handle process(pi.hProcess),thread(pi.hThread);
 if(!AssignProcessToJobObject(job.h,process.h)||ResumeThread(thread.h)==DWORD(-1)){TerminateProcess(process.h,125);if(WaitForSingleObject(process.h,5000)!=WAIT_OBJECT_0)throw aide_host::Unsafe("Unattached native child termination unconfirmed");throw std::runtime_error("Cannot supervise native child process");}
 DWORD waited=WAIT_TIMEOUT,code=1;auto deadline=GetTickCount64()+timeout;bool cancelled=false;
 while(true){auto now=GetTickCount64();if(now>=deadline)break;waited=WaitForSingleObject(process.h,DWORD(std::min<ULONGLONG>(250,deadline-now)));if(waited!=WAIT_TIMEOUT)break;if(nativeInterrupted()){cancelled=true;break;}}
 if(waited!=WAIT_OBJECT_0){if(!TerminateJobObject(job.h,124)||WaitForSingleObject(process.h,5000)!=WAIT_OBJECT_0)throw aide_host::Unsafe("Native child termination unconfirmed; action stopped");code=124;}else GetExitCodeProcess(process.h,&code);
 if(!TerminateJobObject(job.h,code))throw aide_host::Unsafe("Native process tree termination unconfirmed");
 bool stopped=false;for(int i=0;i<50;i++){JOBOBJECT_BASIC_ACCOUNTING_INFORMATION accounting{};if(!QueryInformationJobObject(job.h,JobObjectBasicAccountingInformation,&accounting,sizeof(accounting),nullptr))break;if(!accounting.ActiveProcesses){stopped=true;break;}Sleep(100);}if(!stopped)throw aide_host::Unsafe("Native process tree still active; action stopped");
 Result r{code,read(out),read(err)};if(cancelled)throw std::runtime_error("Native operation stopped by operator");return r;
}
void spawn(const fs::path& exe,const Args& args,const fs::path& log){
 SECURITY_ATTRIBUTES sa{sizeof(sa),nullptr,TRUE};
 Handle output(CreateFileW(log.c_str(),FILE_APPEND_DATA,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,&sa,OPEN_ALWAYS,FILE_ATTRIBUTE_NORMAL,nullptr));
 Handle input(CreateFileW(L"NUL",GENERIC_READ,FILE_SHARE_READ|FILE_SHARE_WRITE,&sa,OPEN_EXISTING,0,nullptr));
 if(!output.valid()||!input.valid())throw std::runtime_error("Cannot open helper log");
 std::wstring cmd=quote(exe.wstring());for(auto& a:args)cmd+=L" "+quote(a);
 Startup si(input.h,output.h,output.h);
 PROCESS_INFORMATION pi{};if(!CreateProcessW(exe.c_str(),cmd.data(),nullptr,nullptr,TRUE,CREATE_NO_WINDOW|EXTENDED_STARTUPINFO_PRESENT,nullptr,nullptr,&si.info.StartupInfo,&pi))throw std::runtime_error("Cannot launch native background helper");
 CloseHandle(pi.hThread);CloseHandle(pi.hProcess);
}
fs::path findExe(const std::wstring& name){std::wstring b(32768,0);DWORD n=SearchPathW(nullptr,name.c_str(),nullptr,DWORD(b.size()),b.data(),nullptr);if(n&&n<b.size()){b.resize(n);return fs::path(b);}return {};}
fs::path installedBrowser(){
 for(auto name:{L"msedge.exe",L"opera.exe",L"chrome.exe",L"firefox.exe"}){
  for(auto hive:{HKEY_CURRENT_USER,HKEY_LOCAL_MACHINE}){
   std::wstring key=L"Software\\Microsoft\\Windows\\CurrentVersion\\App Paths\\"+std::wstring(name);DWORD type=0,size=0;
   if(RegGetValueW(hive,key.c_str(),nullptr,RRF_RT_REG_SZ|RRF_RT_REG_EXPAND_SZ,&type,nullptr,&size)!=ERROR_SUCCESS)continue;
   std::wstring path(size/sizeof(wchar_t),0);if(RegGetValueW(hive,key.c_str(),nullptr,RRF_RT_REG_SZ|RRF_RT_REG_EXPAND_SZ,&type,path.data(),&size)!=ERROR_SUCCESS)continue;
   path.resize(wcslen(path.c_str()));if(path.size()>1&&path.front()==L'"'&&path.back()==L'"')path=path.substr(1,path.size()-2);if(fs::is_regular_file(fs::path(path)))return fs::path(path);
  }
 }
 for(auto path:{fs::path(env(L"LOCALAPPDATA"))/L"Programs/Opera/opera.exe",fs::path(env(L"ProgramFiles"))/L"Opera/opera.exe",fs::path(env(L"ProgramFiles(x86)"))/L"Microsoft/Edge/Application/msedge.exe",fs::path(env(L"ProgramFiles"))/L"Microsoft/Edge/Application/msedge.exe"})if(fs::is_regular_file(path))return path;
 return {};
}
std::string sha256(const fs::path& path){
 BCRYPT_ALG_HANDLE alg=nullptr;BCRYPT_HASH_HANDLE hash=nullptr;
 if(BCryptOpenAlgorithmProvider(&alg,BCRYPT_SHA256_ALGORITHM,nullptr,0)<0)throw std::runtime_error("SHA256 unavailable");
 if(BCryptCreateHash(alg,&hash,nullptr,0,nullptr,0,0)<0){BCryptCloseAlgorithmProvider(alg,0);throw std::runtime_error("SHA256 initialization failed");}
 std::ifstream f(path,std::ios::binary);char buf[65536];bool ok=bool(f);
 while(f.read(buf,sizeof(buf))||f.gcount()){if(BCryptHashData(hash,reinterpret_cast<PUCHAR>(buf),ULONG(f.gcount()),0)<0){ok=false;break;}}
 if(!f.eof())ok=false;unsigned char digest[32];if(BCryptFinishHash(hash,digest,32,0)<0)ok=false;BCryptDestroyHash(hash);BCryptCloseAlgorithmProvider(alg,0);
 if(!ok)throw std::runtime_error("Cannot hash file");const char* hex="0123456789abcdef";std::string result;for(auto c:digest){result+=hex[c>>4];result+=hex[c&15];}return result;
}
bool digest(const std::string& s){return std::regex_match(s,std::regex("[a-f0-9]{64}"));}
bool imageId(const std::string& s){return s.size()==71&&s.substr(0,7)=="sha256:"&&digest(s.substr(7));}
std::string loginLink(const std::string& base,const std::string& token){
 if(token.size()<32||token.size()>4096||std::any_of(token.begin(),token.end(),[](unsigned char c){return c<32||c==127;}))return base;
 const char* hex="0123456789ABCDEF";std::string encoded;
 for(unsigned char c:token){if((c>='a'&&c<='z')||(c>='A'&&c<='Z')||(c>='0'&&c<='9')||c=='-'||c=='_'||c=='.'||c=='~')encoded+=char(c);else{encoded+='%';encoded+=hex[c>>4];encoded+=hex[c&15];}}
 return base+"#token="+encoded;
}
std::map<std::string,std::string> dotenv(const fs::path& p){
 std::map<std::string,std::string> values;if(!fs::exists(p))return values;
 std::istringstream in(read(p));std::string line;while(std::getline(in,line)){line=trim(line);if(line.rfind("export ",0)==0)line=trim(line.substr(7));auto n=line.find('=');if(n==line.npos||line[0]=='#')continue;
 auto k=trim(line.substr(0,n)),v=trim(line.substr(n+1));if(v.size()>=2&&((v.front()=='"'&&v.back()=='"')||(v.front()=='\''&&v.back()=='\'')))v=v.substr(1,v.size()-2);values[k]=v;}return values;
}
void setting(const fs::path& root,const std::string& name,const std::string& value){
 auto file=root/L".env";std::string text=fs::exists(file)?read(file):"";std::regex re("(^|\\n)[ \\t]*(export[ \\t]+)?"+name+"[ \\t]*=[^\\r\\n]*");
 std::string result;size_t offset=0;bool found=false;for(std::sregex_iterator i(text.begin(),text.end(),re),end;i!=end;++i){auto& match=*i;result.append(text,offset,size_t(match.position())-offset);result+=match[1].str()+name+"="+value;offset=size_t(match.position()+match.length());found=true;}
 if(found){result+=text.substr(offset);text=std::move(result);}else text+=(text.empty()?"":"\r\n")+name+"="+value+"\r\n";write(file,text);
}
void privateDirectory(const fs::path& p){
 realPath(p);fs::create_directories(p);
 HANDLE raw=nullptr;if(!OpenProcessToken(GetCurrentProcess(),TOKEN_QUERY,&raw))throw std::runtime_error("Cannot identify current Windows user");Handle token(raw);
 DWORD n=0;GetTokenInformation(token.h,TokenUser,nullptr,0,&n);std::vector<BYTE> b(n);if(!GetTokenInformation(token.h,TokenUser,b.data(),n,&n))throw std::runtime_error("Cannot read Windows user SID");
 LPWSTR sid=nullptr;if(!ConvertSidToStringSidW(reinterpret_cast<TOKEN_USER*>(b.data())->User.Sid,&sid))throw std::runtime_error("Cannot format Windows user SID");std::wstring sddl=L"D:P(A;OICI;FA;;;"+std::wstring(sid)+L")";LocalFree(sid);
 PSECURITY_DESCRIPTOR sd=nullptr;if(!ConvertStringSecurityDescriptorToSecurityDescriptorW(sddl.c_str(),SDDL_REVISION_1,&sd,nullptr))throw std::runtime_error("Cannot create private data ACL");
 BOOL present=FALSE,def=FALSE;PACL acl=nullptr;GetSecurityDescriptorDacl(sd,&present,&acl,&def);
 DWORD code=SetNamedSecurityInfoW(const_cast<LPWSTR>(p.c_str()),SE_FILE_OBJECT,DACL_SECURITY_INFORMATION|PROTECTED_DACL_SECURITY_INFORMATION,nullptr,nullptr,acl,nullptr);LocalFree(sd);
 if(code!=ERROR_SUCCESS)throw std::runtime_error("Cannot protect launcher data ACL");
}
bool portFree(int port){SOCKET s=socket(AF_INET,SOCK_STREAM,IPPROTO_TCP);if(s==INVALID_SOCKET)return false;BOOL exclusive=TRUE;setsockopt(s,SOL_SOCKET,SO_EXCLUSIVEADDRUSE,reinterpret_cast<char*>(&exclusive),sizeof(exclusive));sockaddr_in a{};a.sin_family=AF_INET;a.sin_addr.s_addr=htonl(INADDR_LOOPBACK);a.sin_port=htons(USHORT(port));bool ok=bind(s,reinterpret_cast<sockaddr*>(&a),sizeof(a))==0;closesocket(s);return ok;}
std::string label(const json& m,const std::string& key){if(!m.contains("Config")||!m["Config"].contains("Labels")||!m["Config"]["Labels"].is_object())return {};return m["Config"]["Labels"].value(key,"");}
bool runtime(const json& m,const std::string& platform){
 return m.is_object()&&imageId(m.value("Id",""))&&m.value("Os","")+"/"+m.value("Architecture","")==platform&&digest(label(m,"aide.srcsha"))&&(label(m,"aide.cad-runtime")=="8.0.1.1.0"||(label(m,"aide.runtime-profile")=="slim"&&label(m,"aide.cad-runtime").empty()));
}
#include "native_http.hpp"
class Launcher {
public:
 fs::path root,docker,composeExe;bool noBrowser=false,force=false,offline=false,deferHelpers=false,allowDesktop=true;std::string platform,image;std::map<std::string,std::string> settings;
 explicit Launcher(fs::path r):root(fs::absolute(r)){}
 void say(const std::string& s){std::cout<<s<<std::endl;std::ofstream f(stateRoot/L"launcher.log",std::ios::app);f<<s<<'\n';}
 Result d(Args a,DWORD timeout=120000){return run(docker,a,timeout);}
 std::string nativeRoot(){auto path=fs::weakly_canonical(root).wstring();if(!path.empty())CharLowerBuffW(path.data(),DWORD(path.size()));return utf8(path);}
 void cleanOwnedContainers(const std::string& operation={}){
  struct CleanupMode{bool previous;CleanupMode():previous(observeNativeStop){observeNativeStop=false;}~CleanupMode(){observeNativeStop=previous;}}cleanupMode;
  auto list=[&]{Args args={L"ps",L"-aq",L"--no-trunc",L"--filter",wide("label=aide.native.root="+nativeRoot())};if(!operation.empty())args.insert(args.end(),{L"--filter",wide("label=aide.native.operation="+operation)});auto result=d(args,15000);if(result.code)throw aide_host::Unsafe("Cannot confirm owned temporary container state");return trim(result.out);};
  std::istringstream ids(list());std::string id;while(ids>>id){if(!digest(id))throw aide_host::Unsafe("Invalid owned temporary container identity");auto response=d({L"inspect",wide(id),L"--format",L"{{json .}}"},15000);if(response.code){if(list().empty())return;throw aide_host::Unsafe("Cannot inspect owned temporary container");}auto value=parseJson(response.out);auto nonce=label(value,"aide.native.operation");if(!std::regex_match(nonce,std::regex("[a-f0-9]{32}"))||label(value,"aide.native.root")!=nativeRoot()||value.value("Name","")!="/aide-native-"+nonce)throw aide_host::Unsafe("Temporary container ownership mismatch; cleanup refused");
   d({L"stop",L"--time",L"5",wide(id)},15000);d({L"rm",L"--force",wide(id)},15000);}
  for(int i=0;i<30;i++){if(list().empty())return;Sleep(100);}throw aide_host::Unsafe("Owned temporary container exit is unconfirmed");
 }
 Result ownedRun(Args arguments,DWORD timeout){
  if(arguments.empty()||arguments[0]!=L"run")throw std::runtime_error("Invalid owned container request");std::array<unsigned char,16> bytes{};if(BCryptGenRandom(nullptr,bytes.data(),ULONG(bytes.size()),BCRYPT_USE_SYSTEM_PREFERRED_RNG)<0)throw std::runtime_error("Cannot identify owned temporary container");std::string nonce;const char* hex="0123456789abcdef";for(auto c:bytes){nonce+=hex[c>>4];nonce+=hex[c&15];}
  arguments.insert(arguments.begin()+1,{L"--name",wide("aide-native-"+nonce),L"--label",wide("aide.native.root="+nativeRoot()),L"--label",wide("aide.native.operation="+nonce)});
  try{auto result=d(arguments,timeout);cleanOwnedContainers(nonce);return result;}catch(const aide_host::Unsafe&){throw;}catch(...){cleanOwnedContainers(nonce);throw;}
 }
 Result compose(Args a,DWORD timeout=120000){if(!composeExe.empty())return run(composeExe,a,timeout);a.insert(a.begin(),L"compose");return d(std::move(a),timeout);}
 void required(const Result& r,const char* message){if(r.code)throw std::runtime_error(std::string(message)+" (exit "+std::to_string(r.code)+")");}
 json inspect(const std::string& ref){auto r=d({L"image",L"inspect",wide(ref),L"--format",L"{{json .}}"});return r.code?json():parseJson(r.out);}
 json config(){auto r=compose({L"config",L"--format",L"json"});required(r,"Cannot resolve Compose configuration");return parseJson(r.out);}
 std::string configured(const std::string& key){auto value=utf8(env(wide(key).c_str()));return value.empty()?settings[key]:value;}
 void defaults(const std::string& key,const fs::path& value){if(configured(key).empty()){
  auto literal=utf8(value.wstring());std::string escaped;for(char c:literal){escaped+=c;if(c=='$')escaped+='$';}
  setting(root,key,json(escaped).dump());settings[key]=literal;
 }}
 void restore(bool requireDocker=true){
  setenvw(L"AIDE_WINDOWS_LAUNCHER_MODE",L"native");
  setenvw(L"AIDE_NATIVE_LAUNCHER_EXE",module().wstring());
  privateDirectory(root/L".aide-data");stateRoot=root/L".aide-data"/L"native";privateDirectory(stateRoot);
  for(auto relative:{L".env",L".aide-image",L".aide-data/launcher.env",L".aide-data/image",L".aide-data/compose-project",L".aide-data/storage"})realPath(root/relative);
  if(!fs::exists(root/L".env")&&fs::exists(root/L".aide-data/launcher.env"))write(root/L".env",read(root/L".aide-data/launcher.env"));
  if(!fs::exists(root/L".aide-image")&&fs::exists(root/L".aide-data/image"))write(root/L".aide-image",read(root/L".aide-data/image"));
  settings=dotenv(root/L".env");offline=fs::exists(root/L".aide-image");
  if(offline){std::istringstream marker(read(root/L".aide-image"));std::string ref;marker>>ref;if(!ref.empty())setenvw(L"AIDE_IMAGE",wide(ref));}
  else if(configured("AIDE_IMAGE").empty())setenvw(L"AIDE_IMAGE",L"aide:local");
  if(!fs::exists(root/L"compose.yaml"))write(root/L"compose.yaml",embedded_compose);
  if(configured("COMPOSE_PROJECT_NAME").empty()&&fs::exists(root/L".aide-data/compose-project"))setenvw(L"COMPOSE_PROJECT_NAME",wide(trim(read(root/L".aide-data/compose-project"))));
  if(env(L"COMPOSE_FILE").empty())setenvw(L"COMPOSE_FILE",L"compose.yaml");
  if(env(L"HOME").empty())setenvw(L"HOME",env(L"USERPROFILE"));
  bool source=fs::exists(root/L"go.mod");defaults("AIDE_WORKSPACE",source?root:root/L"workspace");defaults("AIDE_CONTEXT",source?root.parent_path()/L"Harness":root/L"context");
  defaults("AIDE_LOCAL_ROOT",fs::path(env(L"USERPROFILE")).root_path());
  fs::current_path(root);
  docker=findExe(L"docker.exe");if(docker.empty())docker=fs::path(env(L"ProgramFiles"))/L"Docker/Docker/resources/bin/docker.exe";
  if(requireDocker&&!fs::exists(docker))throw std::runtime_error("Install Docker Desktop before starting Aide");
  setenvw(L"DOCKER_BUILDKIT",L"1");setenvw(L"BUILDKIT_PROGRESS",L"plain");setenvw(L"COMPOSE_PROGRESS",L"plain");
 }
 void projectIdentity(){
  if(!configured("COMPOSE_PROJECT_NAME").empty())return;
  auto found=d({L"ps",L"-aq",L"--no-trunc",L"--filter",L"label=com.docker.compose.service=aide"});required(found,"Cannot inspect prior installation identity");
  std::istringstream ids(found.out);std::string id,project;int count=0;
  while(ids>>id){if(!digest(id))throw std::runtime_error("Invalid legacy container identity");auto result=d({L"inspect",wide(id),L"--format",L"{{json .Config.Labels}}"});required(result,"Cannot inspect legacy project labels");auto labels=parseJson(result.out);if(aide_image::field(labels,"com.docker.compose.oneoff")=="True"||aide_image::field(labels,"com.docker.compose.oneoff")=="true")continue;
   if(!aide_installation::owns(root,aide_image::field(labels,"com.docker.compose.project.working_dir")))continue;project=aide_image::field(labels,"com.docker.compose.project");count++;}
  if(count>1)throw std::runtime_error("Multiple legacy owners require an explicit Compose project");
  // Orphaned global volumes do not prove that this new root owns them. An
  // explicit --project / COMPOSE_PROJECT_NAME can select an intended migration.
  if(project.empty()){auto identity=fs::weakly_canonical(root).wstring();if(!identity.empty())CharLowerBuffW(identity.data(),DWORD(identity.size()));auto file=stateRoot/L"root-identity";write(file,utf8(identity));project="aide-native-"+sha256(file).substr(0,16);}
  if(!std::regex_match(project,std::regex("[a-z0-9][a-z0-9_-]*")))throw std::runtime_error("Invalid recovered Compose project identity");setenvw(L"COMPOSE_PROJECT_NAME",wide(project));
 }
 void disk(bool building=false){
  uint64_t minimum=uint64_t(building?20:10)*1024*1024*1024;
  for(auto path:{root,fs::path(env(L"SystemRoot")),fs::path(env(L"LOCALAPPDATA"))})if(!path.empty()){
   auto drive=fs::absolute(path).root_path().wstring();if(drive.rfind(L"\\\\",0)==0)throw std::runtime_error("Native disk budget requires a local installation drive");ULARGE_INTEGER available{};
   if(!GetDiskFreeSpaceExW(drive.c_str(),&available,nullptr,nullptr)||available.QuadPart<minimum)throw std::runtime_error(building?"Native source build needs 20 GiB free on installation/system/profile drives":"Native startup needs 10 GiB free on installation/system/profile drives");}
 }
 void engine(bool startDesktop=true){
  auto info=d({L"info",L"--format",L"{{json .}}"},15000);
  if(info.code&&!startDesktop)throw std::runtime_error("Docker engine is unavailable; background worker will retry without opening Desktop");
  if(info.code){auto desktop=fs::path(env(L"ProgramFiles"))/L"Docker/Docker/Docker Desktop.exe";if(fs::exists(desktop)){say("Starting Docker Desktop...");spawn(desktop,{},stateRoot/L"docker-desktop.log");}
   for(int i=0;i<60&&info.code;i++){Sleep(1000);info=d({L"info",L"--format",L"{{json .}}"},5000);}}
  required(info,"Docker engine is not ready");auto data=parseJson(info.out);platform=data.value("OSType","")+"/"+data.value("Architecture","");
  if(platform=="linux/x86_64")platform="linux/amd64";if(platform=="linux/aarch64")platform="linux/arm64";
  if(platform!="linux/amd64"&&platform!="linux/arm64")throw std::runtime_error("Aide requires a Linux Docker engine (amd64/arm64)");
  if(data.value("MemTotal",uint64_t(0))<uint64_t(4)*1024*1024*1024)throw std::runtime_error("Docker requires at least 4 GiB memory");
  if(compose({L"version"}).code){
   for(auto path:{fs::path(env(L"ProgramFiles"))/L"Docker/cli-plugins/docker-compose.exe",fs::path(env(L"USERPROFILE"))/L".docker/cli-plugins/docker-compose.exe",fs::path(env(L"ProgramData"))/L"Docker/cli-plugins/docker-compose.exe",findExe(L"docker-compose.exe")})if(!path.empty()&&fs::is_regular_file(path)&&!run(path,{L"version"}).code){composeExe=path;break;}
   if(composeExe.empty())throw std::runtime_error("Docker Compose is unavailable; install Docker Desktop with its Compose plugin");
  }
  projectIdentity();
  // An explicit/stored project must still belong to this root. A fresh exe
  // must never stop/adopt another installation that happens to use "aide".
  auto project=config().value("name","");if(!std::regex_match(project,std::regex("[a-z0-9][a-z0-9_-]*")))throw std::runtime_error("Invalid Compose project identity");
  auto owners=d({L"ps",L"-aq",L"--no-trunc",L"--filter",wide("label=com.docker.compose.project="+project)});required(owners,"Cannot inspect Compose project ownership");
  std::istringstream ids(owners.out);std::string id;while(ids>>id){
   if(!digest(id))throw std::runtime_error("Invalid project container identity");
   auto record=d({L"inspect",wide(id),L"--format",L"{{json .Config.Labels}}"});required(record,"Cannot inspect project container owner");
   auto labels=parseJson(record.out);if(!aide_installation::owns(root,aide_image::field(labels,"com.docker.compose.project.working_dir")))throw std::runtime_error("Compose project belongs to another or unknown installation; refusing action");
  }
  auto resolved=config();for(auto& volume:resolved["services"]["aide"]["volumes"]){auto target=volume.value("target","");if(volume.value("type","")=="bind"&&(target=="/workspace"||target=="/context"||target=="/local")){auto source=aide_host::composeLiteral(volume.value("source",""));if(source.empty())throw std::runtime_error("Resolved directory mount is empty");fs::create_directories(fs::u8path(source));}}
 }
 void save(){if(fs::exists(root/L".env"))write(root/L".aide-data/launcher.env",read(root/L".env"));if(fs::exists(root/L".aide-image"))write(root/L".aide-data/image",read(root/L".aide-image"));}
 void rollbackTag(const std::string& target,const std::string& next){auto old=inspect(target);if(!old.is_null()&&old.value("Id","")!=next)required(d({L"image",L"tag",wide(old.value("Id","")),wide("aide:rollback-native-"+std::to_string(GetTickCount64()))}),"Cannot retain rollback image");}
 std::string loadVerified(const fs::path& archive,const std::string& expected,const std::string& osarch){
  auto loaded=d({L"image",L"load",L"-i",archive.wstring()},900000);required(loaded,"Docker image import failed");
  auto m=inspect(expected);if(m.is_null())m=inspect(aide_image::receipt(loaded.out));if(!runtime(m,osarch))throw std::runtime_error("Imported image runtime/platform mismatch; activation refused");return m.value("Id","");
 }
 // Shared native downloader for pinned snapshots and tagged thin packages.
 void downloadFile(const fs::path& destination,const std::string& url,const std::string& expected,uint64_t budget){
  realPath(destination);fs::create_directories(destination.parent_path());auto part=stateRoot/(L"official-download-"+std::to_wstring(GetCurrentProcessId())+L"-"+std::to_wstring(GetTickCount64()));
  struct Cleanup{fs::path p;~Cleanup(){std::error_code error;fs::remove(p,error);}}cleanup{part};
  {Handle output(CreateFileW(part.c_str(),GENERIC_WRITE,0,nullptr,CREATE_NEW,FILE_ATTRIBUTE_NORMAL,nullptr));if(!output.valid())throw std::runtime_error("Cannot create owned Release download");std::string next=url;bool complete=false;
   for(int redirects=0;redirects<8;redirects++){
    auto response=aide_http::request(next,"GET","","",[&](const char* bytes,size_t count){DWORD written=0;if(!WriteFile(output.h,bytes,DWORD(count),&written,nullptr)||written!=count)throw std::runtime_error("Cannot save native Release download");},budget);
    if(response.status>=300&&response.status<400&&!response.location.empty()){next=response.location;continue;}if(response.status!=200)throw std::runtime_error("Official Release download was refused");complete=true;break;}
   if(!complete||!FlushFileBuffers(output.h))throw std::runtime_error("Official Release download did not complete");}
  if(!expected.empty()&&sha256(part)!=expected)throw std::runtime_error("Release archive SHA256 mismatch");fs::rename(part,destination);
 }
 void offlineImage(){
  std::istringstream in(read(root/L".aide-image"));std::string ref,id,osarch,tag;in>>ref>>id>>osarch>>tag;
  if(!imageId(id)||osarch!=platform||!std::regex_match(ref,std::regex("[A-Za-z0-9][A-Za-z0-9._:/-]*")))throw std::runtime_error("Offline image marker identity/platform is invalid");
  auto metadata=inspect(ref);if(metadata.is_null()||metadata.value("Id","")!=id){
   auto byId=inspect(id);if(byId.is_null()){
    auto manifest=root/L"docker-images/manifest.json";std::string name,expected;
    if(fs::exists(manifest)){
     auto m=parseJson(read(manifest));name=m.value("imageArchive","");if(fs::path(name).filename()!=fs::path(name)||m.value("imageId","")!=id||m.value("platform","")!=platform)throw std::runtime_error("Offline archive manifest mismatch");
    }else{
     if(!std::regex_match(tag,std::regex("v[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+-RC[0-9]+")))throw std::runtime_error("Offline image missing; supply the complete matching Release archive and manifest");
     name="aide-"+tag+"-linux-"+platform.substr(6)+"-image.tar.gz";
    }
    auto archive=root/L"docker-images"/wide(name),sums=root/L"docker-images/SHA256SUMS";realPath(archive);realPath(sums);
    if(fs::exists(sums))expected=aide_host::checksum(read(sums,1048576),name);
    else{
     if(tag.empty())throw std::runtime_error("Offline archive SHA256SUMS is missing");
     auto metadata=stateRoot/(L"release-sums-"+std::to_wstring(GetCurrentProcessId())+L"-"+std::to_wstring(GetTickCount64()));struct Cleanup{fs::path p;~Cleanup(){std::error_code error;fs::remove(p,error);}}cleanup{metadata};
     downloadFile(metadata,"https://github.com/skyelan1999/aide/releases/download/"+tag+"/SHA256SUMS","",1048576);expected=aide_host::checksum(read(metadata,1048576),name);
    }
    if(!fs::exists(archive)){if(tag.empty())throw std::runtime_error("Matching offline image archive is missing");say("Downloading the tagged Release image with native HTTPS...");downloadFile(archive,"https://github.com/skyelan1999/aide/releases/download/"+tag+"/"+name,expected,uint64_t(64)<<30);}
    if(sha256(archive)!=expected)throw std::runtime_error("Offline image SHA256 mismatch");id=loadVerified(archive,id,platform);
   }
   rollbackTag(ref,id);required(d({L"image",L"tag",wide(id),wide(ref)}),"Cannot activate offline image");
  }
  if(!runtime(inspect(id),platform))throw std::runtime_error("Offline runtime/platform/source labels are invalid");image=id;setenvw(L"AIDE_IMAGE",wide(image));
 }
 void releaseImage(const std::string& seed=""){
  auto extraction=inspect(seed.empty()?image:seed);if(image.rfind("sha256:",0)==0)image="aide:local";
  if(!extraction.is_null()&&runtime(extraction,platform)){
   auto response=aide_http::request("https://api.github.com/repos/skyelan1999/aide/releases?per_page=100","GET","","",{},8*1024*1024);
   if(response.status!=200)throw std::runtime_error("Cannot check official Release metadata; existing runtime retained");
   auto releases=parseJson(response.body);json selected;std::string published,tag,url,hash;
   for(auto& r:releases){if(r.value("draft",false)||!r.contains("published_at")||!r["published_at"].is_string())continue;auto t=r.value("tag_name","");if(!std::regex_match(t,std::regex("v[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+(?:-RC[0-9]+)?")))continue;
    for(auto& a:r["assets"])if(a.value("name","")=="aide-"+t+"-full-linux-"+platform.substr(6)+".zip"&&r["published_at"].get<std::string>()>published){selected=a;tag=t;published=r["published_at"].get<std::string>();}}
   if(selected.is_null())throw std::runtime_error("No matching official full Release package");url=selected.value("browser_download_url","");hash=selected.value("digest","");
   if(url!="https://github.com/skyelan1999/aide/releases/download/"+tag+"/aide-"+tag+"-full-linux-"+platform.substr(6)+".zip"||hash.rfind("sha256:",0)!=0||!digest(hash.substr(7)))throw std::runtime_error("Invalid official Release digest or URL");
   auto receipt=stateRoot/L"latest-release.json";if(fs::exists(receipt)){auto saved=parseJson(read(receipt));auto cached=inspect(saved.value("imageId",""));if(saved.value("tag","")==tag&&saved.value("digest","")==hash&&runtime(cached,platform)){image=cached.value("Id","");setenvw(L"AIDE_IMAGE",wide(image));say("Using cached verified latest Release: "+tag);return;}}
   auto stage=stateRoot/(L"latest-release-"+std::to_wstring(GetCurrentProcessId())+L"-"+std::to_wstring(GetTickCount64()));fs::create_directory(stage);struct Cleanup{fs::path p;~Cleanup(){std::error_code e;fs::remove_all(p,e);}}cleanup{stage};
   auto zip=stage/L"package.zip",archive=stage/L"image.tar.gz";say("Downloading latest verified full Release: "+tag);downloadFile(zip,url,hash.substr(7),uint64_t(2)<<30);
   auto extracted=ownedRun({L"run",L"--rm",L"--pull",L"never",L"--network",L"none",L"--user",L"0:0",L"--memory",L"256m",L"--cpus",L"1",L"-v",stage.wstring()+L":/stage",L"--entrypoint",L"python3",wide(extraction.value("Id","")),L"-c",wide(embedded_host_data),L"extract-full",L"/stage/package.zip",L"/stage/image.tar.gz",wide(tag),wide(platform)},1200000);required(extracted,"Full Release package verification failed");
   auto metadata=parseJson(extracted.out);auto canonical=loadVerified(archive,metadata.value("imageId",""),platform);rollbackTag(image,canonical);required(d({L"image",L"tag",wide(canonical),wide(image)}),"Cannot activate latest Release");write(receipt,json{{"tag",tag},{"digest",hash},{"imageId",canonical}}.dump());image=canonical;setenvw(L"AIDE_IMAGE",wide(image));return;
  }
  auto manifest=root/L"docker/prebuilt-images.tsv";
  std::istringstream lines(fs::exists(manifest)?read(manifest):std::string(embedded_prebuilt_manifest));std::string line,id,src,hash,url,kind;int count=0;
  while(std::getline(lines,line)){std::istringstream row(line);std::string p;row>>p;if(p==platform){std::string extra;row>>id>>src>>hash>>url>>kind;if(row>>extra)throw std::runtime_error("Invalid Release manifest");count++;}}
  if(count!=1||!imageId(id)||!digest(src)||!digest(hash)||kind!="bundle"||!std::regex_match(url,std::regex("https://github\\.com/skyelan1999/aide/releases/download/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+\\.tar\\.gz")))throw std::runtime_error("Invalid Release manifest identity or URL");
  auto m=inspect(id);if(m.is_null()){
   auto archive=root/L"docker-images"/wide(url.substr(url.find_last_of('/')+1));fs::create_directories(archive.parent_path());
   if(!fs::exists(archive)){say("Downloading SHA256-pinned official runtime with native HTTPS...");downloadFile(archive,url,hash,uint64_t(64)<<30);}
   if(sha256(archive)!=hash)throw std::runtime_error("Cached Release archive SHA256 mismatch");
   auto dir=stateRoot/(L"release-import-"+std::to_wstring(GetCurrentProcessId())+L"-"+std::to_wstring(GetTickCount64()));if(!fs::create_directory(dir))throw std::runtime_error("Cannot create owned import directory");
   struct Cleanup{fs::path p;~Cleanup(){std::error_code error;fs::remove_all(p,error);}}cleanup{dir};auto inner=dir/L"aide-local.tar";
   {Handle output(CreateFileW(inner.c_str(),GENERIC_WRITE,0,nullptr,CREATE_NEW,FILE_ATTRIBUTE_NORMAL,nullptr));if(!output.valid())throw std::runtime_error("Cannot create image member destination");
    aide_archive::extract(archive,[&](const unsigned char* bytes,size_t count){DWORD written=0;if(!WriteFile(output.h,bytes,DWORD(count),&written,nullptr)||written!=count)throw std::runtime_error("Cannot write extracted image member");});}
   // docker load can retag its embedded repository names before validation.
   rollbackTag(image,id);
   auto loaded=d({L"image",L"load",L"-i",inner.wstring()},900000);required(loaded,"Docker image import failed");m=inspect(id);if(m.is_null())m=inspect(aide_image::receipt(loaded.out));
  }
  auto canonical=aide_image::canonical(m,platform,src);
  // Downgrading a populated modern installation is not an automatic recovery.
  auto configFile=root/L".aide-data/storage/data/config/settings.json";
  if(fs::exists(configFile)){auto s=parseJson(read(configFile));if(s.contains("models")&&s["models"].is_array()&&s["models"].size()>20)throw std::runtime_error("Old Release fallback cannot read this model configuration; restore a compatible newer local image");}
  rollbackTag(image,canonical);required(d({L"image",L"tag",wide(canonical),wide(image)}),"Cannot activate verified Release");image=canonical;setenvw(L"AIDE_IMAGE",wide(image));say("Using verified Release snapshot.");
 }
 void selectImage(){
  if(offline){offlineImage();return;}
  auto c=config();image=c["services"]["aide"].value("image","");if(image.empty())throw std::runtime_error("Compose has no application image");
  bool source=fs::exists(root/L"Dockerfile")&&fs::exists(root/L"go.mod")&&env(L"AIDE_BUILD_FROM_SOURCE")!=L"0";std::string expectedSource;
  if(source){auto fingerprint=stateRoot/L"source-input.txt";std::string hashes;for(const auto& name:aide_source::inputs(root,parseJson(embedded_source_manifest)))hashes+=sha256(root/fs::u8path(name))+"  "+name+"\n";write(fingerprint,hashes);expectedSource=sha256(fingerprint);}
  auto m=inspect(image);bool build=force||env(L"AIDE_BUILD_FROM_SOURCE")==L"1"||env(L"AIDE_FORCE_REBUILD")==L"1";
  if(!build&&!m.is_null()&&runtime(m,platform)&&(!source||label(m,"aide.srcsha")==expectedSource)){say("Using validated immutable local runtime.");image=m.value("Id","");setenvw(L"AIDE_IMAGE",wide(image));return;}
  if(source){
   std::smatch versionMatch;auto versionText=read(root/L"version.md");if(!std::regex_search(versionText,versionMatch,std::regex("[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+ RC[0-9]+")))throw std::runtime_error("Source version.md has no valid version");setenvw(L"AIDE_VERSION",wide(versionMatch[0].str()));
   auto seed=inspect("aide:dependencies-v2");if(!seed.is_null()&&label(seed,"aide.local-base")=="v2"){setenvw(L"AIDE_DOCKERFILE",L"Dockerfile.local");setenvw(L"AIDE_BUILD_NETWORK",L"none");}
   setenvw(L"AIDE_SRC_SHA",wide(expectedSource));if(image.rfind("sha256:",0)==0){image="aide:local";setenvw(L"AIDE_IMAGE",wide(image));}
   bool space=true;try{disk(true);}catch(const std::exception&){if(env(L"AIDE_RELEASE_FALLBACK")==L"0")throw;space=false;say("Insufficient source build space; checking latest verified Release.");}if(space){say("Building source runtime locally...");auto result=compose({L"build",L"aide"},3600000);if(!result.code){auto built=inspect(image);if(!runtime(built,platform)||label(built,"aide.srcsha")!=expectedSource)throw std::runtime_error("Built runtime identity/source fingerprint did not verify");image=built.value("Id","");setenvw(L"AIDE_IMAGE",wide(image));return;}}
   if(env(L"AIDE_RELEASE_FALLBACK")==L"0")throw std::runtime_error("Source build failed; Release fallback disabled");say("Source build failed; checking verified Release fallback.");
  }
  releaseImage(m.is_null()?"":m.value("Id",""));setenvw(L"AIDE_IMAGE",wide(image));
 }
 int mappedPort(){auto r=compose({L"port",L"aide",L"8080"});std::smatch m;auto s=trim(r.out);return !r.code&&std::regex_search(s,m,std::regex(":([0-9]+)$"))?std::stoi(m[1]):0;}
 std::string container(){auto r=compose({L"ps",L"--status",L"running",L"-q",L"aide"});return r.code?"":trim(r.out);}
 void port(){
  auto c=config();std::vector<int> ports;for(auto& p:c["services"]["aide"]["ports"])if(p.value("target",0)==8080){auto published=p["published"];std::string s=published.is_string()?published.get<std::string>():published.dump();if(!std::regex_match(s,std::regex("[0-9]{1,5}")))throw std::runtime_error("Invalid application port");ports.push_back(std::stoi(s));}
  if(ports.size()!=1||ports[0]<1||ports[0]>65535)throw std::runtime_error("Expected one valid published application port");int requested=ports[0];
  if(!container().empty()&&mappedPort()==requested)return;if(portFree(requested))return;
  int selected=0;for(int p=requested+1;p<=std::min(requested+100,65535);p++)if(portFree(p)){selected=p;break;}
  if(!selected)throw std::runtime_error("No free port in the next 100 ports; set AIDE_PORT in .env");
  setenvw(L"AIDE_PORT",std::to_wstring(selected));setting(root,"AIDE_PORT",std::to_string(selected));say("Port "+std::to_string(requested)+" is occupied; this install uses "+std::to_string(selected)+". Existing services are retained.");
 }
 void data(){
  auto storage=root/L".aide-data/storage";if(fs::exists(storage)&&!fs::exists(storage/L"migration.json"))throw std::runtime_error("Existing storage has no migration receipt; refusing to overwrite data");
  auto project=config().value("name","");if(!std::regex_match(project,std::regex("[a-z0-9][a-z0-9_-]*")))throw std::runtime_error("Invalid Compose project identity");
  Args sources;std::string owner;bool stopped=false;
  if(!fs::exists(storage)){
   auto found=d({L"ps",L"-aq",L"--no-trunc",L"--filter",L"label=com.docker.compose.service=aide",L"--filter",wide("label=com.docker.compose.project="+project)});required(found,"Cannot inspect legacy data owners");
   std::istringstream ids(found.out);std::string id;json previous;
   while(ids>>id){if(!digest(id))throw std::runtime_error("Invalid legacy data owner");auto result=d({L"inspect",wide(id),L"--format",L"{{json .}}"});required(result,"Cannot inspect legacy data owner");auto record=parseJson(result.out);auto oneoff=label(record,"com.docker.compose.oneoff");if(oneoff=="True"||oneoff=="true")continue;
    if(!owner.empty()||!aide_installation::owns(root,label(record,"com.docker.compose.project.working_dir")))throw std::runtime_error("Ambiguous or unrelated legacy data owner");owner=id;previous=record;}
   for(auto target:{"data","home"}){
    std::string kind,source,destination=std::string(target)=="data"?"/data":"/home/aide";
    if(!previous.is_null()&&previous.contains("Mounts"))for(auto& mount:previous["Mounts"])if(mount.value("Destination","")==destination){if(!source.empty())throw std::runtime_error("Duplicate legacy data mount");kind=mount.value("Type","");source=kind=="volume"?mount.value("Name",""):mount.value("Source","");if(source.empty())throw std::runtime_error("Legacy mount source is missing");}
    if(source.empty()){auto volume=project+"_aide-"+target;if(!d({L"volume",L"inspect",wide(volume)}).code){kind="volume";source=volume;}}
    if(source.empty()&&std::string(target)=="data"&&fs::exists(root/L".data")){realPath(root/L".data");kind="bind";source=utf8((root/L".data").wstring());}
    if(!source.empty()){
     if((kind!="volume"&&kind!="bind")||source.find_first_of("\r\n")!=source.npos||(source.find(':')!=source.npos&&!std::regex_match(source,std::regex("[A-Za-z]:[\\\\/][^:]*"))))throw std::runtime_error("Invalid legacy source mount");
     {
      auto users=d({L"ps",L"-q",L"--no-trunc",L"--filter",wide("volume="+source)});required(users,"Cannot inspect legacy volume users");std::istringstream active(users.out);std::string user;while(active>>user)if(user!=owner)throw std::runtime_error("Legacy volume is used by a different container; migration refused");}
     sources.insert(sources.end(),{L"-v",wide(source+":/legacy/"+target+":ro")});
    }
   }
   if(!owner.empty()&&previous.contains("State")&&previous["State"].value("Running",false)){
    auto companion=embeddedHelper("local-mounts");stopDirectory(companion);
    required(d({L"exec",wide(owner),L"python3",L"-c",wide(embedded_host_data),L"check-idle",L"/data"},30000),"Active or unreadable work prevents native migration");
    write(root/L".cache/aide-manual-stop","");required(d({L"stop",L"--time",L"30",wide(owner)}),"Cannot stop the verified legacy owner for migration");stopped=true;
   }
  }
  auto helper=stateRoot/L"launcher-data.py";write(helper,embedded_data);
  Args arguments={L"run",L"--rm",L"--pull",L"never",L"--network",L"none",L"--user",L"0:0",L"-v",(root/L".aide-data").wstring()+L":/persist",L"-v",helper.wstring()+L":/tool.py:ro"};arguments.insert(arguments.end(),sources.begin(),sources.end());
  arguments.insert(arguments.end(),{L"--entrypoint",L"python3",wide(image),L"/tool.py",L"--platform",L"windows",L"--seed-home"});
  for(size_t i=0;i<sources.size();i+=2){auto value=utf8(sources[i+1]);if(value.find(":/legacy/data:ro")!=value.npos)arguments.insert(arguments.end(),{L"--data",L"/legacy/data"});if(value.find(":/legacy/home:ro")!=value.npos)arguments.insert(arguments.end(),{L"--home",L"/legacy/home"});}
  try{required(ownedRun(arguments,300000),"Persistent data preparation failed; source data retained");if(!fs::exists(storage/L"migration.json"))throw std::runtime_error("Native data preparation did not commit a migration receipt");}
  catch(const aide_host::Unsafe&){throw;}
  catch(...){if(stopped)required(d({L"start",wide(owner)}),"Data preparation failed and the previous owner could not restart");throw;}
  write(root/L".aide-data/compose-project",project+"\n");save();
 }
 std::string hostArch(){
  return embedded_host_arch;
 }
 fs::path embeddedHelper(const std::string& role){
  auto folder=root/L".aide-data"/wide(role);privateDirectory(folder);auto name="aide-"+role+"-windows-"+hostArch()+".exe";auto file=folder/wide(name);realPath(file);
  const unsigned char* begin=role=="local-mounts"?aide_local_mounts_begin:aide_message_lan_begin;
  const unsigned char* end=role=="local-mounts"?aide_local_mounts_end:aide_message_lan_end;
  auto expected=role=="local-mounts"?embedded_local_mounts_sha256:embedded_message_lan_sha256;
  if(fs::exists(file)&&sha256(file)==expected)return file;
  auto stage=stateRoot/(wide(name)+L".new-"+std::to_wstring(GetCurrentProcessId())+L"-"+std::to_wstring(GetTickCount64()));realPath(stage);
  struct Cleanup{fs::path p;~Cleanup(){std::error_code error;fs::remove(p,error);}}cleanup{stage};
  {Handle output(CreateFileW(stage.c_str(),GENERIC_WRITE,0,nullptr,CREATE_NEW,FILE_ATTRIBUTE_NORMAL,nullptr));if(!output.valid())throw std::runtime_error("Cannot materialize embedded host component");
   size_t size=reinterpret_cast<uintptr_t>(end)-reinterpret_cast<uintptr_t>(begin);for(size_t offset=0;offset<size;){DWORD count=0,n=DWORD(std::min<size_t>(65536,size-offset));if(!WriteFile(output.h,begin+offset,n,&count,nullptr)||count!=n)throw std::runtime_error("Cannot write embedded host component");offset+=n;}if(!FlushFileBuffers(output.h))throw std::runtime_error("Cannot commit embedded host component");}
  if(sha256(stage)!=expected)throw std::runtime_error("Embedded host component SHA256 mismatch");
  if(role=="local-mounts")stopDirectory(stage);else{auto dir=root/L".aide-data/storage/data/messages/lan-bridge";if(fs::exists(dir))required(run(stage,{L"-stop",L"-dir",dir.wstring()}),"Previous LAN component did not stop");}
  if(!MoveFileExW(stage.c_str(),file.c_str(),MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH))throw std::runtime_error("Cannot install verified embedded host component");return file;
 }
 fs::path directoryHelper;bool directoryAvailable=false;
 void stopDirectory(const fs::path& program){
  auto pidFile=root/L".aide-data/storage/data/local-mounts/serve.pid";DWORD pid=0;
  if(fs::exists(pidFile)){auto value=trim(read(pidFile,128));if(std::regex_match(value,std::regex("[1-9][0-9]{0,9}"))){auto parsed=std::stoull(value);if(parsed<=MAXDWORD)pid=DWORD(parsed);}}
  auto stopped=run(program,{L"-mode",L"stop",L"-root",root.wstring()});if(!stopped.code)return;
  if(pid&&(stopped.err+stopped.out).find("serve.pid")!=std::string::npos&&!fs::exists(pidFile)&&fs::exists(pidFile.parent_path()/L"stop")){
   HANDLE handle=OpenProcess(SYNCHRONIZE|PROCESS_QUERY_LIMITED_INFORMATION,FALSE,pid);bool gone=false;
   if(!handle)gone=GetLastError()==ERROR_INVALID_PARAMETER;else{gone=WaitForSingleObject(handle,0)==WAIT_OBJECT_0;CloseHandle(handle);}
   if(gone){say("Directory helper already exited; recovered legacy PID cleanup race.");return;}
  }
  throw std::runtime_error("Directory helper did not stop; update refused");
 }
 void prepareDirectory(){
  auto target=root/L".aide-data/local-mounts";directoryHelper=embeddedHelper("local-mounts");stopDirectory(directoryHelper);setenvw(L"AIDE_LOCAL_MOUNTS_COMPOSE",composeExe.empty()?L"docker":composeExe.wstring());
  auto prepared=run(directoryHelper,{L"-mode",L"prepare",L"-root",root.wstring(),L"-docker",docker.wstring()});required(prepared,"Directory mounts preparation failed");
  auto override=trim(prepared.out);if(!override.empty()){auto file=fs::absolute(fs::u8path(override));if(file.lexically_normal()!=(target/L"compose.json").lexically_normal())throw std::runtime_error("Unexpected directory helper Compose path");auto composeFiles=env(L"COMPOSE_FILE");if(composeFiles.find(file.wstring())==std::wstring::npos)setenvw(L"COMPOSE_FILE",composeFiles+L";"+file.wstring());}
  directoryAvailable=true;
 }
 std::string binding(const wchar_t* port){auto r=compose({L"port",L"aide",port});required(r,"Missing helper port mapping");auto value=trim(r.out);if(!std::regex_match(value,std::regex("127\\.0\\.0\\.1:[0-9]{1,5}")))throw std::runtime_error("Helper mapping must be loopback only");return value;}
 void codexHost(){
  auto helper=root/L"scripts/codex-host-control.cjs",node=findExe(L"node.exe");
  if(!fs::exists(helper))return;
  if(node.empty()){say("Codex Bridge requires host Node.js 18+; other Aide features remain available.");return;}
  realPath(helper);
  // Keep the headless manager in the resident host job. run() deliberately
  // kills descendants on completion and must not be used for this bootstrap.
  spawn(node,{helper.wstring(),L"start",L"--root",root.wstring(),L"--image",wide(image),L"--docker",docker.wstring()},stateRoot/L"codex-host.log");
 }
 void helpers(){
  if(directoryAvailable)spawn(directoryHelper,{L"-root",root.wstring(),L"-docker",docker.wstring()},stateRoot/L"directory-helper.log");
  try{
   auto cid=container();auto actual=d({L"inspect",wide(cid),L"--format",L"{{.Image}}"});required(actual,"Cannot identify running image");auto selected=inspect(image);if(selected.is_null()||selected.value("Id","")!=trim(actual.out))throw std::runtime_error("Running image changed; refusing helper extraction");
   auto binary=embeddedHelper("message-lan"),dir=root/L".aide-data/storage/data/messages/lan-bridge";privateDirectory(dir);
   required(run(binary,{L"-stop",L"-dir",dir.wstring()}),"Old LAN helper could not stop");
   spawn(binary,{L"-dir",dir.wstring(),L"-target",wide(binding(L"45840")),L"-capture-target",wide(binding(L"45841"))},stateRoot/L"lan-helper.log");
   bool ready=false;for(int i=0;i<30;i++){if(!run(binary,{L"-status",L"-dir",dir.wstring()},5000).code){ready=true;break;}Sleep(100);}if(!ready)throw std::runtime_error("LAN helper not ready; check its local log");say("LAN bridge ready.");
   try{codexHost();}catch(const std::exception&){say("Codex host component unavailable; see native/codex-host.log.");}
  }catch(const std::exception&){throw;}
 }
 bool healthy(){auto h=compose({L"exec",L"-T",L"aide",L"curl",L"-fsSk",L"--connect-timeout",L"2",L"--max-time",L"3",L"https://127.0.0.1:8080/healthz"},6000);if(h.code)return false;auto j=json::parse(h.out,nullptr,false);return j.is_object()&&j.value("service","")=="aide"&&j.value("status","")=="ok";}
 void waitHealthy(){for(int i=0;i<60;i++){if(nativeInterrupted())throw std::runtime_error("Health wait stopped by operator");if(healthy())return;Sleep(1000);}throw std::runtime_error("Health check failed; inspect Docker logs for this project");}
 void browser(){int port=mappedPort();if(!port)throw std::runtime_error("Cannot resolve the ready service address");std::string base="https://localhost:"+std::to_string(port)+"/";say("Aide ready: "+base);
  // Direct argv avoids an extra shell parser. Use the mounted private token as
  // a final fallback for Docker exec output/transport problems; never log it.
  auto token=compose({L"exec",L"-T",L"aide",L"cat",L"/data/auth/access-token"});
  if(token.code)token=compose({L"exec",L"-T",L"aide",L"cat",L"/data/access-token"});
  std::string t=token.code?"":trim(token.out);
  if(t.empty()){auto file=root/L".aide-data/storage/data/auth/access-token";if(fs::exists(file))t=trim(read(file,4096));}
  std::string url=loginLink(base,t);say(url==base?"Warning: automatic sign-in token unavailable; use the login page.":"Automatic sign-in link ready (token attached; omitted from logs).");
  if(noBrowser||env(L"AIDE_OPEN_BROWSER")==L"0")return;
  if(reinterpret_cast<INT_PTR>(ShellExecuteW(nullptr,L"open",wide(url).c_str(),nullptr,nullptr,SW_SHOWNORMAL))<=32){auto browser=installedBrowser();if(browser.empty()){say("Warning: no usable installed browser; open the displayed address manually.");return;}spawn(browser,{wide(url)},fs::path(L"NUL"));say("HTTPS association unavailable; opened the installed browser directly.");}
 }
 void start(){
  if(nativeInterrupted()||fs::exists(stateRoot/L"stop"))throw std::runtime_error("Native startup stopped by operator");disk();engine(allowDesktop);cleanOwnedContainers();write(root/L".cache/aide-manual-stop","");selectImage();data();port();prepareDirectory();if(nativeInterrupted())throw std::runtime_error("Native startup stopped by operator");say("Starting Aide...");required(compose({L"up",L"-d",L"--no-build",L"--pull",L"never"},300000),"Compose startup failed");if(!deferHelpers)helpers();
  waitHealthy();save();if(!deferHelpers)browser();
 }
 bool stop(){write(root/L".cache/aide-manual-stop","");write(stateRoot/L"stop","");for(auto kind:{"local-mounts","message-lan"}){auto name=std::string("aide-")+kind+"-windows-"+hostArch()+".exe";auto file=root/L".aide-data"/wide(kind)/wide(name);if(fs::exists(file)){file=embeddedHelper(kind);if(std::string(kind)=="local-mounts")stopDirectory(file);else required(run(file,{L"-stop",L"-dir",(root/L".aide-data/storage/data/messages/lan-bridge").wstring()}),"Host helper did not stop");}}
  if(docker.empty()||!fs::exists(docker)||d({L"info"},15000).code){say("Native host tasks stopped; Docker engine/CLI is unavailable, data retained.");return false;}engine(false);cleanOwnedContainers();required(compose({L"stop"}),"Cannot stop current Aide project");say("Current Aide project stopped; data retained.");return true;}
 void cleanCache(bool dryRun){engine(false);if(dryRun){say("Dry run: docker builder prune --all --force (build cache only).");return;}required(d({L"builder",L"prune",L"--all",L"--force"},900000),"Build cache cleanup failed");say("Unused build cache removed; containers, images, volumes and user files retained.");}
};
#include "host_windows.hpp"
#include "host_scheduler.hpp"
int selfTest(){
 auto base=std::string("https://localhost:8098/");auto token=std::string(32,'a')+"+/=#";auto link=loginLink(base,token);if(link!=base+"#token="+std::string(32,'a')+"%2B%2F%3D%23"||loginLink(base,"short")!=base)throw std::runtime_error("Automatic sign-in URL encoding failed");
 bool redacted=false;try{parseJson("{private-configuration-sentinel");}catch(const std::exception& e){redacted=std::string(e.what()).find("sentinel")==std::string::npos;}if(!redacted)throw std::runtime_error("JSON error redaction failed");
 std::vector<std::wstring> values={L"",L"simple",L"中文 路径",L"a\"b",L"C:\\directory with spaces\\",L"x&echo injected",L"$() ; `"};
 for(auto& value:values){auto line=L"test.exe "+quote(value);int count=0;auto parsed=CommandLineToArgvW(line.c_str(),&count);if(!parsed||count!=2||std::wstring(parsed[1])!=value)throw std::runtime_error("Windows argument roundtrip failed");LocalFree(parsed);}
 json image={{"Id","sha256:"+std::string(64,'a')},{"Os","linux"},{"Architecture","amd64"},{"Config",{{"Labels",{{"aide.srcsha",std::string(64,'b')},{"aide.runtime-profile","slim"}}}}}};
 if(!runtime(image,"linux/amd64")||runtime(image,"linux/arm64"))throw std::runtime_error("Native image platform validation failed");image["Config"]["Labels"]["aide.srcsha"]="invalid";if(runtime(image,"linux/amd64"))throw std::runtime_error("Native source validation failed");
 auto current=fs::current_path();if(!aide_installation::owns(current,utf8(current.wstring()))||aide_installation::owns(current,""))throw std::runtime_error("Installation ownership self-test failed");
 std::cout<<"PASS native Unicode/quote roundtrip and image validation; installation-ownership-v2\n";return 0;
}
int chooseDirectory(){
 HRESULT initialized=CoInitializeEx(nullptr,COINIT_APARTMENTTHREADED|COINIT_DISABLE_OLE1DDE);if(FAILED(initialized))throw std::runtime_error("Cannot initialize native directory picker");
 IFileOpenDialog* dialog=nullptr;HRESULT result=CoCreateInstance(CLSID_FileOpenDialog,nullptr,CLSCTX_INPROC_SERVER,IID_PPV_ARGS(&dialog));
 if(FAILED(result)){CoUninitialize();throw std::runtime_error("Native directory picker unavailable");}
 DWORD options=0;dialog->GetOptions(&options);dialog->SetOptions(options|FOS_PICKFOLDERS|FOS_FORCEFILESYSTEM);dialog->SetTitle(L"Choose a directory for Aide");result=dialog->Show(nullptr);
 std::wstring selected;if(SUCCEEDED(result)){IShellItem* item=nullptr;if(SUCCEEDED(dialog->GetResult(&item))){PWSTR path=nullptr;if(SUCCEEDED(item->GetDisplayName(SIGDN_FILESYSPATH,&path))){selected=path;CoTaskMemFree(path);}item->Release();}}
 dialog->Release();CoUninitialize();if(selected.empty())return 2;std::cout<<utf8(selected);return 0;
}
BOOL WINAPI nativeConsoleControl(DWORD event){
 if(event==CTRL_C_EVENT||event==CTRL_BREAK_EVENT||event==CTRL_CLOSE_EVENT){if(!nativeStopPath.empty()){HANDLE file=CreateFileW(nativeStopPath.c_str(),GENERIC_WRITE,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,nullptr,OPEN_ALWAYS,FILE_ATTRIBUTE_NORMAL,nullptr);if(file!=INVALID_HANDLE_VALUE)CloseHandle(file);}return TRUE;}return FALSE;
}
int wmain(int argc,wchar_t** argv){
 SetConsoleOutputCP(CP_UTF8);WSADATA data{};if(WSAStartup(MAKEWORD(2,2),&data))return 1;
 bool pause=false;DWORD ids[2];if(GetConsoleProcessList(ids,2)==1)pause=true;
 try{
  fs::path root=module().parent_path();std::wstring action=L"start";std::string generation;bool noBrowser=false,force=false,dryRun=false;
  for(int i=1;i<argc;i++){
   std::wstring a=argv[i];
   if(a==L"--root"&&i+1<argc)root=fs::absolute(argv[++i]);
   else if(a==L"--generation"&&i+1<argc)generation=utf8(argv[++i]);
   else if(a==L"--project"&&i+1<argc){auto value=utf8(argv[++i]);if(!std::regex_match(value,std::regex("[a-z0-9][a-z0-9_-]*")))throw std::runtime_error("Invalid explicit Compose project");setenvw(L"COMPOSE_PROJECT_NAME",wide(value));}
   else if(a==L"--no-browser")noBrowser=true;
   else if(a==L"--force-build")force=true;
   else if(a==L"--dry-run")dryRun=true;
   else if(a==L"--self-test")return selfTest();
   else if(a==L"--licenses"){std::cout<<embedded_json_license<<'\n'<<embedded_miniz_license<<'\n'<<embedded_helper_licenses;return 0;}
   else if(a==L"start"||a==L"stop"||a==L"status"||a==L"host-status"||a==L"host-stop"||a==L"supervise"||a==L"worker-port"||a==L"worker-update"||a==L"open"||a==L"refresh-helpers"||a==L"clean-cache"||a==L"choose-directory"||a==L"confirm-directory-restart")action=a;
   else if(a==L"--help"){std::cout<<"aide-launcher.exe [start|stop|status|host-status|host-stop|supervise|open|clean-cache] [--root DIR] [--project NAME] [--no-browser] [--force-build] [--dry-run] [--licenses]\n";return 0;}
   else throw std::runtime_error("Unknown launcher argument");
  }
  if(action==L"choose-directory")return chooseDirectory();
  if(action==L"confirm-directory-restart")return MessageBoxW(nullptr,L"Apply saved directory permissions and recreate Aide? Save your work first. Running tasks and phone connections will stop.",L"Aide directories",MB_OKCANCEL|MB_ICONWARNING|MB_DEFBUTTON2)==IDOK?0:2;
  if(dryRun&&action==L"start"){std::cout<<"Dry run: native environment/image validation, data preparation, current-project startup, embedded components and user-session workers; no commands executed.\n";return 0;}
  if(action==L"host-status"){
   stateRoot=fs::absolute(root)/L".aide-data/native";realPath(root/L".aide-data");realPath(stateRoot);
   auto status=fs::exists(stateRoot)?nativeControllerStatus():json{{"running",false}};
   for(auto role:{"port","update"}){auto file=stateRoot/wide(std::string("task-")+role+".json");realPath(file);if(fs::exists(file)){auto task=parseJson(read(file,8192));task["running"]=nativeAlive(task);status["tasks"][role]=task;}}
   status["stopRequired"]=fs::exists(stateRoot/L"host-stop-required");std::cout<<status.dump(2)<<'\n';return 0;
  }
  Launcher app(root);app.noBrowser=noBrowser;app.force=force;privateDirectory(app.root/L".aide-data");stateRoot=app.root/L".aide-data/native";privateDirectory(stateRoot);nativeStopPath=stateRoot/L"stop";
  if(action==L"supervise"){pause=false;SetConsoleCtrlHandler(nativeConsoleControl,TRUE);return nativeSupervise(app);}
  if(action==L"worker-port"||action==L"worker-update"){pause=false;if(!std::regex_match(generation,std::regex("[a-f0-9]{32}")))throw std::runtime_error("Missing native worker generation");return nativeWorkerLoop(app,action==L"worker-port"?"port":"update",generation);}
  if(action==L"status"||action==L"open"){NativeLock lifecycle(stateRoot/L"startup.lock");if(!lifecycle.owns())throw std::runtime_error("Lifecycle is busy; use host-status for nonblocking task status");app.restore();app.engine(false);if(!app.healthy())return 1;if(action==L"status")app.noBrowser=true;app.browser();return 0;}
  std::unique_ptr<NativeLock> control;
  if(action==L"stop"||action==L"host-stop"){
   for(int i=0;i<150;i++){write(nativeStopPath,"");control=std::make_unique<NativeLock>(stateRoot/L"control.lock");if(control->owns())break;control.reset();Sleep(100);}if(!control)throw std::runtime_error("Another explicit host action has not stopped yet");nativeStopController();
  }else{control=std::make_unique<NativeLock>(stateRoot/L"control.lock");if(!control->owns())throw std::runtime_error("Another explicit native host action is active");}
  if(action==L"host-stop")return 0;
  {
   NativeLock lifecycle(stateRoot/L"startup.lock");if(!lifecycle.owns())throw std::runtime_error("Another lifecycle operation owns this installation");app.restore(action!=L"stop");
   if(action==L"stop"){if(app.stop())nativeClearDirectoryBlocker();}
   else if(action==L"clean-cache")app.cleanCache(dryRun);
   else if(action==L"refresh-helpers"){nativeCheckPreviousAgent(app);observeNativeStop=true;app.engine(false);auto cid=app.container();auto m=app.d({L"inspect",wide(cid),L"--format",L"{{.Image}}"});app.required(m,"Cannot resolve running image");app.image=trim(m.out);setenvw(L"AIDE_IMAGE",wide(app.image));app.helpers();}
   else{nativeCheckPreviousAgent(app);nativeRestoreInterruptedMarker(app);fs::remove(nativeStopPath);observeNativeStop=true;app.deferHelpers=true;app.start();write(stateRoot/L"helper-refresh",std::to_string(GetCurrentProcessId())+"-"+std::to_string(GetTickCount64()));}
  }
  if(action==L"start"){
   if(nativeStopping())throw std::runtime_error("Native startup stopped by operator");
   try{nativeLaunchController(app);}catch(const aide_host::Unsafe&){throw;}catch(const std::exception& e){app.say(std::string("Background warning: ")+e.what());}
   if(!nativeStopping())app.browser();
  }
  return 0;
 }catch(const std::exception& e){if(dynamic_cast<const aide_host::Unsafe*>(&e)&&!stateRoot.empty())try{write(stateRoot/L"host-stop-required",aide_host::diagnostic(e.what()));}catch(...){}std::cerr<<"Native host action failed: "<<aide_host::diagnostic(e.what(),{},1024)<<std::endl;if(pause){std::cerr<<"Press Enter to close...";std::cin.get();}return 1;}
}
