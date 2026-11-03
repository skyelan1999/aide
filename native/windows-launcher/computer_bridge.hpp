#pragma once
// Private Windows host bridge for Aide computer control. Only loopback and
// Docker private-network peers with the per-install token can reach it.
#include <winsock2.h>
#include <windows.h>
#include <gdiplus.h>
#include <bcrypt.h>
#include <wincrypt.h>
#include <objidl.h>
#include <ws2tcpip.h>
#include <atomic>
#include <climits>
#include <cstdint>
#include <cwctype>
#include <map>
#include <thread>
#include <mutex>
#include <vector>
#include <sstream>

namespace aide_desktop {
class ComputerBridge {
 SOCKET listener=INVALID_SOCKET;
 std::string token;
 std::atomic<bool> stopping{false};
 std::thread acceptThread;
 std::mutex clientsMutex,operationMutex;
 std::vector<std::thread> clients;
 ULONG_PTR gdiplusToken=0;
 bool winsockStarted=false;
 static constexpr USHORT port=17778;
 struct Target { HWND window=nullptr; DWORD pid=0; std::wstring process,title; };
 static std::wstring lower(std::wstring s){std::transform(s.begin(),s.end(),s.begin(),[](wchar_t c){return std::towlower(c);});return s;}
 static Target frontmost(){
  Target t;t.window=GetForegroundWindow();if(!t.window)throw std::runtime_error("No foreground app");GetWindowThreadProcessId(t.window,&t.pid);if(!t.pid||IsIconic(t.window))throw std::runtime_error("Foreground app is not available");
  HANDLE process=OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION,FALSE,t.pid);if(!process)throw std::runtime_error("Cannot identify foreground app");std::wstring path(32768,0);DWORD size=DWORD(path.size());BOOL ok=QueryFullProcessImageNameW(process,0,path.data(),&size);CloseHandle(process);if(!ok)throw std::runtime_error("Cannot identify foreground app");path.resize(size);auto slash=path.find_last_of(L"\\/");t.process=slash==path.npos?path:path.substr(slash+1);
  wchar_t title[1024]{};int n=GetWindowTextW(t.window,title,1024);if(n>0)t.title.assign(title,size_t(n));return t;
 }
 static Target authorized(const json& input){
  auto apps=input.value("allowedApps",json::array());if(!apps.is_array()||apps.empty()||apps.size()>50)throw std::runtime_error("Allowed app list is invalid");auto target=frontmost();bool match=false;
  for(const auto& item:apps){if(!item.is_string()||item.get<std::string>().size()>480)continue;auto value=lower(wide(item.get<std::string>()));match=match||value==lower(target.process)||value==lower(target.title);}
  if(!match)throw std::runtime_error("Foreground app is not in Aide's allowed app list");return target;
 }
 static bool same(const Target& target){auto now=frontmost();return now.window==target.window&&now.pid==target.pid;}
 static bool privatePeer(SOCKET client){sockaddr_in peer{};int size=sizeof(peer);if(getpeername(client,reinterpret_cast<sockaddr*>(&peer),&size)!=0||peer.sin_family!=AF_INET)return false;auto ip=ntohl(peer.sin_addr.s_addr);auto a=(ip>>24)&255,b=(ip>>16)&255;return a==127||(a==192&&b==168)|| (a==172&&b>=16&&b<=31);}
 static bool sendAll(SOCKET s,const std::string& data){size_t offset=0;while(offset<data.size()){int n=send(s,data.data()+offset,int(std::min<size_t>(data.size()-offset,INT_MAX)),0);if(n<=0)return false;offset+=size_t(n);}return true;}
 static void response(SOCKET s,int status,const json& value){auto body=value.dump();auto reason=status==200?"OK":status==501?"Not Implemented":status==401?"Unauthorized":status==403?"Forbidden":status==404?"Not Found":status==413?"Payload Too Large":"Bad Request";std::string header="HTTP/1.1 "+std::to_string(status)+" "+reason+"\r\nContent-Type: application/json; charset=utf-8\r\nCache-Control: no-store\r\nConnection: close\r\nContent-Length: "+std::to_string(body.size())+"\r\n\r\n";sendAll(s,header+body);}
 static std::string base64(const std::vector<BYTE>& bytes){DWORD n=0;if(!CryptBinaryToStringA(bytes.data(),DWORD(bytes.size()),CRYPT_STRING_BASE64|CRYPT_STRING_NOCRLF,nullptr,&n)||!n)throw std::runtime_error("PNG base64 encoding failed");std::string out(n,'\0');if(!CryptBinaryToStringA(bytes.data(),DWORD(bytes.size()),CRYPT_STRING_BASE64|CRYPT_STRING_NOCRLF,out.data(),&n))throw std::runtime_error("PNG base64 encoding failed");out.resize(n&&out[n-1]=='\0'?n-1:n);return out;}
 static std::vector<BYTE> screenshot(const Target& target){
  RECT rect{};if(!GetWindowRect(target.window,&rect))throw std::runtime_error("Cannot read app window bounds");int width=rect.right-rect.left,height=rect.bottom-rect.top;if(width<1||height<1||width>4096||height>4096||int64_t(width)*height>4000000)throw std::runtime_error("App window exceeds screenshot limits");
  HDC screen=GetDC(nullptr),memory=CreateCompatibleDC(screen);HBITMAP bitmap=CreateCompatibleBitmap(screen,width,height);if(!screen||!memory||!bitmap){if(bitmap)DeleteObject(bitmap);if(memory)DeleteDC(memory);if(screen)ReleaseDC(nullptr,screen);throw std::runtime_error("Cannot allocate screenshot buffer");}
  auto old=SelectObject(memory,bitmap);BOOL captured=PrintWindow(target.window,memory,PW_RENDERFULLCONTENT);SelectObject(memory,old);DeleteDC(memory);ReleaseDC(nullptr,screen);if(!captured){DeleteObject(bitmap);throw std::runtime_error("Windows could not capture this app window");}
  Gdiplus::Bitmap image(bitmap,nullptr);IStream* stream=nullptr;HRESULT hr=CreateStreamOnHGlobal(nullptr,TRUE,&stream);if(FAILED(hr)){DeleteObject(bitmap);throw std::runtime_error("Cannot allocate PNG stream");}
  CLSID png{};CLSIDFromString(L"{557CF406-1A04-11D3-9A73-0000F81EF32E}",&png);auto saved=image.Save(stream,&png,nullptr);DeleteObject(bitmap);if(saved!=Gdiplus::Ok){stream->Release();throw std::runtime_error("PNG capture encoding failed");}
  STATSTG stat{};if(FAILED(stream->Stat(&stat,STATFLAG_NONAME))||stat.cbSize.QuadPart<8||stat.cbSize.QuadPart>2*1024*1024){stream->Release();throw std::runtime_error("Screenshot exceeds 2 MiB");}
  LARGE_INTEGER zero{};stream->Seek(zero,STREAM_SEEK_SET,nullptr);std::vector<BYTE> bytes(size_t(stat.cbSize.QuadPart));ULONG read=0;hr=stream->Read(bytes.data(),ULONG(bytes.size()),&read);stream->Release();if(FAILED(hr)||read!=bytes.size())throw std::runtime_error("PNG capture read failed");return bytes;
 }
 static WORD keyCode(const std::string& key){static const std::map<std::string,WORD> keys{{"ENTER",VK_RETURN},{"TAB",VK_TAB},{"ESCAPE",VK_ESCAPE},{"BACKSPACE",VK_BACK},{"DELETE",VK_DELETE},{"UP",VK_UP},{"DOWN",VK_DOWN},{"LEFT",VK_LEFT},{"RIGHT",VK_RIGHT},{"SPACE",VK_SPACE}};auto it=keys.find(key);if(it==keys.end())throw std::runtime_error("Unsupported key");return it->second;}
 void serve(SOCKET client){
  DWORD timeout=5000;setsockopt(client,SOL_SOCKET,SO_RCVTIMEO,reinterpret_cast<const char*>(&timeout),sizeof(timeout));setsockopt(client,SOL_SOCKET,SO_SNDTIMEO,reinterpret_cast<const char*>(&timeout),sizeof(timeout));
  try{
   if(!privatePeer(client)){response(client,403,{{"ok",false},{"error","Private bridge peer required"}});closesocket(client);return;}
   std::string data;char buffer[4096];size_t split=std::string::npos;size_t contentLength=0;std::string auth,method,path;
   while(split==std::string::npos||data.size()<split+4+contentLength){int n=recv(client,buffer,sizeof(buffer),0);if(n<=0)throw std::runtime_error("Incomplete HTTP request");data.append(buffer,size_t(n));if(data.size()>32768)throw std::runtime_error("Request too large");if(split==std::string::npos){split=data.find("\r\n\r\n");if(split!=std::string::npos){auto lineEnd=data.find("\r\n");std::istringstream first(data.substr(0,lineEnd));first>>method>>path;std::istringstream headers(data.substr(lineEnd+2,split-lineEnd-2));std::string line;while(std::getline(headers,line)){if(!line.empty()&&line.back()=='\r')line.pop_back();auto colon=line.find(':');if(colon==std::string::npos)continue;auto name=lower(wide(line.substr(0,colon)));auto value=line.substr(colon+1);while(!value.empty()&&(value.front()==' '||value.front()=='\t'))value.erase(value.begin());if(name==L"authorization")auth=value;else if(name==L"content-length")contentLength=size_t(std::stoul(value));else if(name==L"transfer-encoding")throw std::runtime_error("Transfer encoding is unsupported");}if(contentLength>16384)throw std::runtime_error("Request body too large");}}}
   if(auth!="Bearer "+token){response(client,401,{{"ok",false},{"error","Unauthorized"}});closesocket(client);return;}
   std::lock_guard<std::mutex> operation(operationMutex);
   if(method=="GET"&&path=="/healthz"){response(client,200,{{"ok",true},{"service","aide-windows-computer-bridge"},{"platform","windows"}});closesocket(client);return;}
   if(method!="POST"){response(client,404,{{"ok",false},{"error","Unsupported operation"}});closesocket(client);return;}
   auto route=path;auto query=route.find('?');if(query!=route.npos)route.resize(query);json input=json::parse(data.substr(split+4,contentLength),nullptr,false);if(!input.is_object()){response(client,400,{{"ok",false},{"error","Invalid JSON"}});closesocket(client);return;}
   if(route=="/v1/inspect"){response(client,501,{{"ok",false},{"error","Windows accessibility inspection is not available in this bridge; use computer_snapshot."}});closesocket(client);return;}
   if(route!="/v1/snapshot"&&route!="/v1/click"&&route!="/v1/type"&&route!="/v1/key"){response(client,404,{{"ok",false},{"error","Unsupported computer operation"}});closesocket(client);return;}
   auto target=authorized(input);
   if(route=="/v1/snapshot"){auto image=screenshot(target);if(!same(target))throw std::runtime_error("Foreground app changed; screenshot discarded");response(client,200,{{"ok",true},{"app",utf8(target.process)},{"mimeType","image/png"},{"imageBase64",base64(image)}});closesocket(client);return;}
   if(!same(target)||!input.value("approved",false))throw std::runtime_error("Aide confirmation missing or foreground app changed");
   if(route=="/v1/click"){int x=input.value("x",-1),y=input.value("y",-1);if(x<0||y<0||x>20000||y>20000||!SetCursorPos(x,y))throw std::runtime_error("Invalid screen coordinates");INPUT click[2]{};click[0].type=click[1].type=INPUT_MOUSE;click[0].mi.dwFlags=MOUSEEVENTF_LEFTDOWN;click[1].mi.dwFlags=MOUSEEVENTF_LEFTUP;if(SendInput(2,click,sizeof(INPUT))!=2)throw std::runtime_error("Windows refused click input");response(client,200,{{"ok",true},{"app",utf8(target.process)},{"clicked",true}});closesocket(client);return;}
   if(route=="/v1/type"){auto text=input.value("text",std::string{});auto wideText=wide(text);if(wideText.size()>4000)throw std::runtime_error("Input exceeds limit");for(wchar_t c:wideText){INPUT events[2]{};events[0].type=events[1].type=INPUT_KEYBOARD;events[0].ki.wScan=WORD(c);events[0].ki.dwFlags=KEYEVENTF_UNICODE;events[1].ki.wScan=WORD(c);events[1].ki.dwFlags=KEYEVENTF_UNICODE|KEYEVENTF_KEYUP;if(SendInput(2,events,sizeof(INPUT))!=2)throw std::runtime_error("Windows refused text input");}response(client,200,{{"ok",true},{"app",utf8(target.process)},{"typed",true}});closesocket(client);return;}
   auto key=keyCode(input.value("key",std::string{}));INPUT events[2]{};events[0].type=events[1].type=INPUT_KEYBOARD;events[0].ki.wVk=events[1].ki.wVk=key;events[1].ki.dwFlags=KEYEVENTF_KEYUP;if(SendInput(2,events,sizeof(INPUT))!=2)throw std::runtime_error("Windows refused key input");response(client,200,{{"ok",true},{"app",utf8(target.process)},{"key",input.value("key",std::string{})}});closesocket(client);return;
  }catch(const std::exception& error){response(client,502,{{"ok",false},{"error",std::string(error.what()).substr(0,500)}});closesocket(client);}
 }
 void loop(){while(!stopping){sockaddr_in peer{};int size=sizeof(peer);SOCKET client=accept(listener,reinterpret_cast<sockaddr*>(&peer),&size);if(client==INVALID_SOCKET){if(stopping)break;Sleep(40);continue;}std::lock_guard<std::mutex> lock(clientsMutex);clients.emplace_back([this,client]{serve(client);});}}
public:
 explicit ComputerBridge(std::string secret):token(std::move(secret)){}
 ~ComputerBridge(){stop();}
 void start(){if(token.size()<32)return;WSADATA wsa{};if(WSAStartup(MAKEWORD(2,2),&wsa)!=0)throw std::runtime_error("Computer bridge network startup failed");winsockStarted=true;Gdiplus::GdiplusStartupInput input;if(Gdiplus::GdiplusStartup(&gdiplusToken,&input,nullptr)!=Gdiplus::Ok)throw std::runtime_error("Screenshot encoder startup failed");listener=socket(AF_INET,SOCK_STREAM,IPPROTO_TCP);if(listener==INVALID_SOCKET)throw std::runtime_error("Computer bridge socket unavailable");BOOL exclusive=TRUE;setsockopt(listener,SOL_SOCKET,SO_EXCLUSIVEADDRUSE,reinterpret_cast<const char*>(&exclusive),sizeof(exclusive));sockaddr_in address{};address.sin_family=AF_INET;address.sin_addr.s_addr=htonl(INADDR_ANY);address.sin_port=htons(port);if(bind(listener,reinterpret_cast<sockaddr*>(&address),sizeof(address))!=0||listen(listener,16)!=0){closesocket(listener);listener=INVALID_SOCKET;throw std::runtime_error("Computer bridge port 17778 is unavailable");}stopping=false;acceptThread=std::thread([this]{loop();});}
 void stop(){stopping=true;if(listener!=INVALID_SOCKET){shutdown(listener,SD_BOTH);closesocket(listener);listener=INVALID_SOCKET;}if(acceptThread.joinable())acceptThread.join();{std::lock_guard<std::mutex> lock(clientsMutex);for(auto& client:clients)if(client.joinable())client.join();clients.clear();}if(gdiplusToken){Gdiplus::GdiplusShutdown(gdiplusToken);gdiplusToken=0;}if(winsockStarted){WSACleanup();winsockStarted=false;}}
};
}
