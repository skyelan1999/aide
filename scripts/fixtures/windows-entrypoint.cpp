// Disposable native boundaries for CMD bootstrap tests. Never shipped.
#include <windows.h>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <string>
#ifdef FIXTURE_REG
#include "../../native/windows-launcher/third_party/json.hpp"
#endif
namespace fs=std::filesystem;
std::wstring env(const wchar_t* n){wchar_t b[32768];DWORD len=GetEnvironmentVariableW(n,b,32768);return len&&len<32768?std::wstring(b,len):L"";}
void log(const std::string& s){std::ofstream out(fs::path(env(L"FIXTURE_CALLS")),std::ios::app);out<<s<<std::endl;}
std::string utf8(const std::wstring& s){if(s.empty())return {};int n=WideCharToMultiByte(CP_UTF8,0,s.data(),int(s.size()),nullptr,0,nullptr,nullptr);std::string out(n,0);WideCharToMultiByte(CP_UTF8,0,s.data(),int(s.size()),out.data(),n,nullptr,nullptr);return out;}
int wmain(int argc,wchar_t** argv){
 if(env(L"FIXTURE_CHECK_PROXY")==L"1"){
  for(auto pair:{std::pair<const wchar_t*,const wchar_t*>{L"HTTP_PROXY",L"FIXTURE_EXPECT_HTTP"},{L"HTTPS_PROXY",L"FIXTURE_EXPECT_HTTPS"},{L"NO_PROXY",L"FIXTURE_EXPECT_NO"}})if(env(pair.first)!=env(pair.second)){log("proxy:wrong-environment");return 27;}
  log("proxy:inherited");
 }
#ifdef FIXTURE_REG
 if(argc==2&&std::wstring(argv[1])==L"--record-env"){
  nlohmann::json values=nlohmann::json::object();for(auto name:{L"HTTP_PROXY",L"HTTPS_PROXY",L"ALL_PROXY",L"NO_PROXY"})values[utf8(name)]=utf8(env(name));
  std::ofstream{fs::path(env(L"FIXTURE_PROXY_RECORD"))}<<values.dump();return 0;
 }
 if(argc<2)return 1;std::wstring key=argv[argc-1],value,type=L"REG_SZ";
 if(key==L"ProxyEnable"){value=env(L"FIXTURE_PROXY_ENABLE")==L"1"?L"0x1":L"0x0";type=L"REG_DWORD";}
 else if(key==L"ProxyServer")value=env(L"FIXTURE_PROXY_SERVER");
 else if(key==L"ProxyOverride")value=env(L"FIXTURE_PROXY_BYPASS");
 else if(key==L"AutoConfigURL")value=env(L"FIXTURE_PROXY_PAC");else return 1;
 if(value.empty())return 1;
 std::cout<<"HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings\n    "<<utf8(key)<<"    "<<utf8(type)<<"    "<<utf8(value)<<"\n";return 0;
#elif defined(FIXTURE_DOCKER)
 std::wstring first=argc>1?argv[1]:L"",second=argc>2?argv[2]:L"";
 std::string a(first.begin(),first.end()),b(second.begin(),second.end());log("docker:"+a+":"+b);
 if(first==L"image"&&second==L"inspect")return 1;
 if(first==L"buildx"&&second==L"build"&&env(L"FIXTURE_BUILD_FAIL")==L"1")return 9;
 if(first==L"create"){for(int i=2;i+1<argc;i++)if(std::wstring(argv[i])==L"--cidfile"){std::ofstream file{fs::path(argv[i+1])};auto bad=env(L"FIXTURE_CID_BAD");file<<(bad==L"short"?std::string(63,'a'):bad==L"nonhex"?std::string(63,'a')+"&":bad==L"extra"?std::string(64,'a')+"\n&echo unsafe>injected":std::string(64,'a'))<<std::endl;}}
 if(first==L"cp")return argc==4&&CopyFileW(env(L"FIXTURE_NEW_EXE").c_str(),argv[3],FALSE)?0:7;
 return 0;
#else
 std::wstring action=argc>1?argv[1]:L"";std::string a(action.begin(),action.end());
 if(action==L"--wait-stop"&&argc==3){
  auto stop=fs::path(argv[2])/L".aide-data/native/stop";log("previous:started");
  for(int i=0;i<300;i++){if(GetFileAttributesW(stop.c_str())!=INVALID_FILE_ATTRIBUTES){Sleep(500);return 0;}Sleep(100);}return 34;
 }
#ifdef FIXTURE_LEGACY
 log("legacy:"+a);return 17;
#else
 if(action==L"--ack-console"&&argc==3){
  FreeConsole();if(!AttachConsole(DWORD(_wtoi(argv[2]))))return 31;
  HANDLE input=CreateFileW(L"CONIN$",GENERIC_READ|GENERIC_WRITE,FILE_SHARE_READ|FILE_SHARE_WRITE,nullptr,OPEN_EXISTING,0,nullptr);
  if(input==INVALID_HANDLE_VALUE)return 32;
  INPUT_RECORD keys[2]{};for(auto& key:keys){key.EventType=KEY_EVENT;key.Event.KeyEvent.wRepeatCount=1;key.Event.KeyEvent.wVirtualKeyCode=VK_RETURN;key.Event.KeyEvent.uChar.UnicodeChar=L'\r';}
  keys[0].Event.KeyEvent.bKeyDown=TRUE;DWORD written=0;BOOL ok=WriteConsoleInputW(input,keys,2,&written);CloseHandle(input);FreeConsole();return ok&&written==2?0:33;
 }
 if(action==L"--launcher-contract"){std::cout<<"aide-launcher-contract=2\n";return 0;}
 log("native:"+a);std::cout<<"Native fixture action finished\n";
 return action==L"host-stop"?0:env(L"FIXTURE_ACTION_FAIL")==L"1"?23:0;
#endif
#endif
}
