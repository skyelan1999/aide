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
#include <algorithm>
#include <climits>
#include <cmath>
#include <cstdint>
#include <cstring>
#include <cwchar>
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
 static std::wstring processName(const Target& t){auto slash=t.process.find_last_of(L"\\/");auto name=slash==t.process.npos?t.process:t.process.substr(slash+1);auto dot=name.find_last_of(L'.');if(dot!=name.npos)name.resize(dot);return name;}
 static std::wstring appRule(const Target& t){return L"exe:"+lower(t.process);}
 static bool interactiveDesktop(){
  HDESK desktop=OpenInputDesktop(0,FALSE,DESKTOP_READOBJECTS);if(!desktop)return false;wchar_t name[128]{};DWORD bytes=0;BOOL ok=GetUserObjectInformationW(desktop,UOI_NAME,name,sizeof(name),&bytes);CloseDesktop(desktop);return ok&&_wcsicmp(name,L"Default")==0;
 }
 static Target fromWindow(HWND window){
  Target t;t.window=window;if(!window)throw std::runtime_error("No app window");GetWindowThreadProcessId(window,&t.pid);if(!t.pid||t.pid==GetCurrentProcessId()||IsIconic(window))throw std::runtime_error("App window is not available");
  HANDLE process=OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION,FALSE,t.pid);if(!process)throw std::runtime_error("Cannot identify foreground app");std::wstring path(32768,0);DWORD size=DWORD(path.size());BOOL ok=QueryFullProcessImageNameW(process,0,path.data(),&size);CloseHandle(process);if(!ok)throw std::runtime_error("Cannot identify foreground app");path.resize(size);auto slash=path.find_last_of(L"\\/");t.process=slash==path.npos?path:path.substr(slash+1);
  wchar_t title[1024]{};int n=GetWindowTextW(window,title,1024);if(n>0)t.title.assign(title,size_t(n));return t;
 }
 static Target frontmost(){if(!interactiveDesktop())throw std::runtime_error("Windows desktop is locked or unavailable; no app was read or controlled");HWND window=GetForegroundWindow();if(!window)throw std::runtime_error("No foreground app");return fromWindow(window);}
 static json identity(const Target& t){return {{"ok",true},{"app",utf8(processName(t))},{"rule",utf8(appRule(t))},{"pid",t.pid}};}
 struct ResolveContext{std::wstring selector;std::map<DWORD,Target> matches;};
 static BOOL CALLBACK collectWindow(HWND window,LPARAM parameter){auto& context=*reinterpret_cast<ResolveContext*>(parameter);if(!IsWindowVisible(window)||GetWindow(window,GW_OWNER))return TRUE;try{auto t=fromWindow(window);if(context.selector.empty()||lower(processName(t))==context.selector||lower(t.process)==context.selector||lower(t.title)==context.selector)context.matches.emplace(t.pid,std::move(t));}catch(...){}return TRUE;}
 static Target resolve(const json& input){auto raw=input.value("app",std::string{});if(raw.empty()||raw.size()>256||raw.find_first_of("\r\n")!=std::string::npos||raw.find('\0')!=std::string::npos)throw std::runtime_error("app must be an exact app name or executable");auto selector=lower(wide(raw));if(!interactiveDesktop())throw std::runtime_error("Windows desktop is locked or unavailable; no app was read or controlled");ResolveContext context{selector,{}};EnumWindows(collectWindow,reinterpret_cast<LPARAM>(&context));if(context.matches.empty())throw std::runtime_error("No matching visible app window; check the exact app name and open the app first");if(context.matches.size()!=1)throw std::runtime_error("App name matches multiple processes; close duplicate instances or choose the foreground app");return context.matches.begin()->second;}
 static json runningApps(){json apps=json::array();if(!interactiveDesktop())return apps;ResolveContext context{L"",{}};EnumWindows(collectWindow,reinterpret_cast<LPARAM>(&context));for(const auto& [pid,target]:context.matches){if(apps.size()>=80)break;apps.push_back(identity(target));}return apps;}
 static Target authorized(const json& input){
  auto apps=input.value("allowedApps",json::array());if(!apps.is_array()||apps.empty()||apps.size()>50)throw std::runtime_error("Allowed app list is invalid");auto target=frontmost();bool match=false;
  for(const auto& item:apps){if(!item.is_string()||item.get<std::string>().size()>480)continue;auto value=lower(wide(item.get<std::string>()));match=match||value==lower(target.process)||value==lower(processName(target))||value==lower(appRule(target));}
  auto expectedPid=input.value("expectedPid",int64_t(0));auto expectedRule=input.value("expectedAppRule",std::string{});
  if(expectedPid<=0||DWORD(expectedPid)!=target.pid||lower(wide(expectedRule))!=lower(appRule(target)))throw std::runtime_error("Authorized app changed; rediscover the app and approve the action again");
  if(!match)throw std::runtime_error("Foreground app is not in Aide's allowed app list");return target;
 }
 static RECT windowBounds(const Target& target){RECT rect{};if(!GetWindowRect(target.window,&rect)||rect.right<=rect.left||rect.bottom<=rect.top)throw std::runtime_error("Cannot read approved app window bounds");return rect;}
 static void verifyExpectedWindow(const Target& target,const json& input){
  auto id=input.value("expectedWindowId",uint64_t(0));if(id&&id!=uint64_t(reinterpret_cast<uintptr_t>(target.window)))throw std::runtime_error("The window from the approved screenshot changed; action was not sent");
  auto it=input.find("expectedWindow");if(it==input.end()||!it->is_object())return;auto rect=windowBounds(target);auto expected=*it;
  auto close=[](double a,double b){return std::abs(a-b)<1.0;};
  if(!expected.contains("x")||!expected.contains("y")||!expected.contains("width")||!expected.contains("height")||!close(expected.value("x",-1.0),rect.left)||!close(expected.value("y",-1.0),rect.top)||!close(expected.value("width",-1.0),rect.right-rect.left)||!close(expected.value("height",-1.0),rect.bottom-rect.top))throw std::runtime_error("The approved window moved or resized; take a new screenshot before acting");
 }
 static bool same(const Target& target){auto now=frontmost();return now.window==target.window&&now.pid==target.pid;}
 static bool privatePeer(SOCKET client){sockaddr_in peer{};int size=sizeof(peer);if(getpeername(client,reinterpret_cast<sockaddr*>(&peer),&size)!=0||peer.sin_family!=AF_INET)return false;auto ip=ntohl(peer.sin_addr.s_addr);auto a=(ip>>24)&255,b=(ip>>16)&255;return a==127||(a==192&&b==168)|| (a==172&&b>=16&&b<=31);}
 static bool sendAll(SOCKET s,const std::string& data){size_t offset=0;while(offset<data.size()){int n=send(s,data.data()+offset,int(std::min<size_t>(data.size()-offset,INT_MAX)),0);if(n<=0)return false;offset+=size_t(n);}return true;}
 static void response(SOCKET s,int status,const json& value){auto body=value.dump();auto reason=status==200?"OK":status==501?"Not Implemented":status==401?"Unauthorized":status==403?"Forbidden":status==404?"Not Found":status==413?"Payload Too Large":"Bad Request";std::string header="HTTP/1.1 "+std::to_string(status)+" "+reason+"\r\nContent-Type: application/json; charset=utf-8\r\nCache-Control: no-store\r\nConnection: close\r\nContent-Length: "+std::to_string(body.size())+"\r\n\r\n";sendAll(s,header+body);}
 static std::string base64(const std::vector<BYTE>& bytes){DWORD n=0;if(!CryptBinaryToStringA(bytes.data(),DWORD(bytes.size()),CRYPT_STRING_BASE64|CRYPT_STRING_NOCRLF,nullptr,&n)||!n)throw std::runtime_error("PNG base64 encoding failed");std::string out(n,'\0');if(!CryptBinaryToStringA(bytes.data(),DWORD(bytes.size()),CRYPT_STRING_BASE64|CRYPT_STRING_NOCRLF,out.data(),&n))throw std::runtime_error("PNG base64 encoding failed");out.resize(n&&out[n-1]=='\0'?n-1:n);return out;}
 static std::vector<BYTE> screenshot(const Target& target,RECT* capturedBounds=nullptr){
  RECT rect{};if(!GetWindowRect(target.window,&rect))throw std::runtime_error("Cannot read app window bounds");int width=rect.right-rect.left,height=rect.bottom-rect.top;if(width<1||height<1||width>4096||height>4096||int64_t(width)*height>4000000)throw std::runtime_error("App window exceeds screenshot limits");if(capturedBounds)*capturedBounds=rect;
  HDC screen=GetDC(nullptr),memory=CreateCompatibleDC(screen);HBITMAP bitmap=CreateCompatibleBitmap(screen,width,height);if(!screen||!memory||!bitmap){if(bitmap)DeleteObject(bitmap);if(memory)DeleteDC(memory);if(screen)ReleaseDC(nullptr,screen);throw std::runtime_error("Cannot allocate screenshot buffer");}
  auto old=SelectObject(memory,bitmap);BOOL captured=PrintWindow(target.window,memory,PW_RENDERFULLCONTENT);SelectObject(memory,old);DeleteDC(memory);ReleaseDC(nullptr,screen);if(!captured){DeleteObject(bitmap);throw std::runtime_error("Windows could not capture this app window");}
  Gdiplus::Bitmap image(bitmap,nullptr);IStream* stream=nullptr;HRESULT hr=CreateStreamOnHGlobal(nullptr,TRUE,&stream);if(FAILED(hr)){DeleteObject(bitmap);throw std::runtime_error("Cannot allocate PNG stream");}
  CLSID png{};CLSIDFromString(L"{557CF406-1A04-11D3-9A73-0000F81EF32E}",&png);auto saved=image.Save(stream,&png,nullptr);DeleteObject(bitmap);if(saved!=Gdiplus::Ok){stream->Release();throw std::runtime_error("PNG capture encoding failed");}
  STATSTG stat{};if(FAILED(stream->Stat(&stat,STATFLAG_NONAME))||stat.cbSize.QuadPart<8||stat.cbSize.QuadPart>2*1024*1024){stream->Release();throw std::runtime_error("Screenshot exceeds 2 MiB");}
  LARGE_INTEGER zero{};stream->Seek(zero,STREAM_SEEK_SET,nullptr);std::vector<BYTE> bytes(size_t(stat.cbSize.QuadPart));ULONG read=0;hr=stream->Read(bytes.data(),ULONG(bytes.size()),&read);stream->Release();if(FAILED(hr)||read!=bytes.size())throw std::runtime_error("PNG capture read failed");return bytes;
 }
 static WORD keyCode(const std::string& key,std::vector<WORD>& modifiers){
  static const std::map<std::string,WORD> keys{{"ENTER",VK_RETURN},{"TAB",VK_TAB},{"ESCAPE",VK_ESCAPE},{"BACKSPACE",VK_BACK},{"DELETE",VK_DELETE},{"UP",VK_UP},{"DOWN",VK_DOWN},{"LEFT",VK_LEFT},{"RIGHT",VK_RIGHT},{"SPACE",VK_SPACE},{"HOME",VK_HOME},{"END",VK_END},{"PAGEUP",VK_PRIOR},{"PAGEDOWN",VK_NEXT}};
  if(auto it=keys.find(key);it!=keys.end())return it->second;
  if(key.size()==1&&((key[0]>='A'&&key[0]<='Z')||(key[0]>='0'&&key[0]<='9')))return WORD(key[0]);
  if(key.size()>=2&&key[0]=='F'){try{int number=std::stoi(key.substr(1));if(number>=1&&number<=20)return WORD(VK_F1+number-1);}catch(...){}}
  if(key.size()==1){SHORT translated=VkKeyScanW(wchar_t(static_cast<unsigned char>(key[0])));if(translated!=-1){BYTE state=HIBYTE(translated);if(state&1)modifiers.push_back(VK_SHIFT);if(state&2)modifiers.push_back(VK_CONTROL);if(state&4)modifiers.push_back(VK_MENU);return LOBYTE(translated);}}
  throw std::runtime_error("Unsupported key");
 }
 static std::vector<INPUT> keyboardChord(const std::string& chord){
  std::vector<std::string> parts;size_t begin=0;for(;;){auto end=chord.find('+',begin);parts.push_back(chord.substr(begin,end==chord.npos?chord.npos:end-begin));if(end==chord.npos)break;begin=end+1;}
  if(parts.empty()||parts.size()>5)throw std::runtime_error("Invalid key chord");std::vector<WORD> modifiers;
  for(size_t i=0;i+1<parts.size();++i){WORD code=parts[i]=="CMD"||parts[i]=="CTRL"?VK_CONTROL:parts[i]=="ALT"?VK_MENU:parts[i]=="SHIFT"?VK_SHIFT:0;if(!code)throw std::runtime_error("Unsupported key modifier");modifiers.push_back(code);}
  WORD key=keyCode(parts.back(),modifiers);std::vector<WORD> unique;for(auto code:modifiers)if(std::find(unique.begin(),unique.end(),code)==unique.end())unique.push_back(code);
  std::vector<INPUT> events;events.reserve(unique.size()*2+2);auto add=[&](WORD code,bool up){INPUT event{};event.type=INPUT_KEYBOARD;event.ki.wVk=code;if(up)event.ki.dwFlags=KEYEVENTF_KEYUP;events.push_back(event);};
  for(auto code:unique)add(code,false);add(key,false);add(key,true);for(auto it=unique.rbegin();it!=unique.rend();++it)add(*it,true);return events;
 }
 void serve(SOCKET client){
  DWORD timeout=5000;setsockopt(client,SOL_SOCKET,SO_RCVTIMEO,reinterpret_cast<const char*>(&timeout),sizeof(timeout));setsockopt(client,SOL_SOCKET,SO_SNDTIMEO,reinterpret_cast<const char*>(&timeout),sizeof(timeout));
  try{
   if(!privatePeer(client)){response(client,403,{{"ok",false},{"error","Private bridge peer required"}});closesocket(client);return;}
   std::string data;char buffer[4096];size_t split=std::string::npos;size_t contentLength=0;std::string auth,method,path;
   while(split==std::string::npos||data.size()<split+4+contentLength){int n=recv(client,buffer,sizeof(buffer),0);if(n<=0)throw std::runtime_error("Incomplete HTTP request");data.append(buffer,size_t(n));if(data.size()>32768)throw std::runtime_error("Request too large");if(split==std::string::npos){split=data.find("\r\n\r\n");if(split!=std::string::npos){auto lineEnd=data.find("\r\n");std::istringstream first(data.substr(0,lineEnd));first>>method>>path;std::istringstream headers(data.substr(lineEnd+2,split-lineEnd-2));std::string line;while(std::getline(headers,line)){if(!line.empty()&&line.back()=='\r')line.pop_back();auto colon=line.find(':');if(colon==std::string::npos)continue;auto name=lower(wide(line.substr(0,colon)));auto value=line.substr(colon+1);while(!value.empty()&&(value.front()==' '||value.front()=='\t'))value.erase(value.begin());if(name==L"authorization")auth=value;else if(name==L"content-length")contentLength=size_t(std::stoul(value));else if(name==L"transfer-encoding")throw std::runtime_error("Transfer encoding is unsupported");}if(contentLength>16384)throw std::runtime_error("Request body too large");}}}
   if(auth!="Bearer "+token){response(client,401,{{"ok",false},{"error","Unauthorized"}});closesocket(client);return;}
   std::lock_guard<std::mutex> operation(operationMutex);
   auto route=path;auto query=route.find('?');if(query!=route.npos)route.resize(query);
   if(method=="GET"&&route=="/healthz"){bool available=interactiveDesktop();response(client,200,{{"ok",true},{"service","aide-windows-computer-bridge"},{"platform","windows"},{"desktopAvailable",available},{"applications",runningApps()},{"targetedApps",true},{"backgroundRead",false},{"backgroundControl",false},{"foregroundInputPolicy","serialized"},{"permissions",{{"screenRecording",{{"required",false},{"granted",true},{"supported",true}}},{"accessibility",{{"required",true},{"granted",false},{"supported",false}}}}}});closesocket(client);return;}
   if(method=="GET"&&route=="/v1/foreground-app"){response(client,200,identity(frontmost()));closesocket(client);return;}
   if(method!="POST"){response(client,404,{{"ok",false},{"error","Unsupported operation"}});closesocket(client);return;}
   json input=json::parse(data.substr(split+4,contentLength),nullptr,false);if(!input.is_object()){response(client,400,{{"ok",false},{"error","Invalid JSON"}});closesocket(client);return;}
   if(route=="/v1/resolve-app"){response(client,200,identity(resolve(input)));closesocket(client);return;}
   if(route=="/v1/inspect"){response(client,501,{{"ok",false},{"error","Windows accessibility inspection is not available in this bridge; use computer_snapshot."}});closesocket(client);return;}
   if(route!="/v1/snapshot"&&route!="/v1/click"&&route!="/v1/type"&&route!="/v1/key"&&route!="/v1/drag"&&route!="/v1/scroll"){response(client,404,{{"ok",false},{"error","Unsupported computer operation"}});closesocket(client);return;}
   auto target=authorized(input);
   if(route=="/v1/snapshot"){RECT rect{};auto image=screenshot(target,&rect);if(!same(target))throw std::runtime_error("Foreground app changed; screenshot discarded");auto current=windowBounds(target);if(std::memcmp(&rect,&current,sizeof(rect))!=0)throw std::runtime_error("App window moved or resized while the screenshot was captured");response(client,200,{{"ok",true},{"app",utf8(processName(target))},{"windowId",reinterpret_cast<uintptr_t>(target.window)},{"window",{{"x",rect.left},{"y",rect.top},{"width",rect.right-rect.left},{"height",rect.bottom-rect.top}}},{"pixelWidth",rect.right-rect.left},{"pixelHeight",rect.bottom-rect.top},{"textLocations",json::array()},{"mimeType","image/png"},{"imageBase64",base64(image)}});closesocket(client);return;}
   if(!same(target)||!input.value("approved",false))throw std::runtime_error("Aide confirmation missing or foreground app changed");verifyExpectedWindow(target,input);
   if(route=="/v1/drag"||route=="/v1/scroll"){
    const int x=input.value("x",-1),y=input.value("y",-1);
    const RECT initial=windowBounds(target);
    auto inside=[&](int px,int py){return px>=initial.left&&px<initial.right&&py>=initial.top&&py<initial.bottom&&px>=-20000&&px<=20000&&py>=-20000&&py<=20000;};
    if(!inside(x,y))throw std::runtime_error("Pointer origin must be inside the approved foreground window");
    auto stable=[&](){if(!same(target))throw std::runtime_error("Foreground app changed; pointer gesture stopped");auto current=windowBounds(target);if(std::memcmp(&initial,&current,sizeof(initial))!=0)throw std::runtime_error("App window moved or resized; pointer gesture stopped");};
    stable();
    if(route=="/v1/scroll"){
     const int dx=input.value("deltaX",0),dy=input.value("deltaY",0);
     if(dx < -2000 || dx > 2000 || dy < -2000 || dy > 2000 || (dx==0&&dy==0))throw std::runtime_error("Scroll amount must be between 1 and 2000 pixels on either axis");
     INPUT events[2]{};UINT count=0;
     if(dy){events[count].type=INPUT_MOUSE;events[count].mi.dwFlags=MOUSEEVENTF_WHEEL;events[count].mi.mouseData=DWORD(-dy*3);++count;}
     if(dx){events[count].type=INPUT_MOUSE;events[count].mi.dwFlags=MOUSEEVENTF_HWHEEL;events[count].mi.mouseData=DWORD(dx*3);++count;}
     if(!SetCursorPos(x,y))throw std::runtime_error("Windows refused pointer positioning");
     stable();
     if(SendInput(count,events,sizeof(INPUT))!=count)throw std::runtime_error("Windows refused scroll input");
     response(client,200,{{"ok",true},{"app",utf8(processName(target))},{"scrolled",true},{"deltaX",dx},{"deltaY",dy}});closesocket(client);return;
    }
    const int toX=input.value("toX",-1),toY=input.value("toY",-1),duration=input.value("durationMs",900);
    if(!inside(toX,toY)||duration<100||duration>2500)throw std::runtime_error("Drag endpoint or duration is invalid");
    if(!SetCursorPos(x,y))throw std::runtime_error("Windows refused pointer positioning");
    stable();
    INPUT down{};down.type=INPUT_MOUSE;down.mi.dwFlags=MOUSEEVENTF_LEFTDOWN;
    if(SendInput(1,&down,sizeof(INPUT))!=1)throw std::runtime_error("Windows refused drag start");
    struct ReleaseLeftButton { bool armed=true; ~ReleaseLeftButton(){if(armed){INPUT up{};up.type=INPUT_MOUSE;up.mi.dwFlags=MOUSEEVENTF_LEFTUP;SendInput(1,&up,sizeof(INPUT));}} } release;
    const int steps=std::max(6,std::min(156,duration/16));
    for(int i=1;i<=steps;++i){
     Sleep(DWORD(std::max(1,duration/steps)));
     stable();
     const double t=double(i)/steps,smooth=t*t*(3.0-2.0*t);
     const int px=int(std::lround(x+(toX-x)*smooth)),py=int(std::lround(y+(toY-y)*smooth));
     if(!SetCursorPos(px,py))throw std::runtime_error("Windows refused drag movement");
     stable();
    }
    stable();
    response(client,200,{{"ok",true},{"app",utf8(processName(target))},{"dragged",true},{"durationMs",duration}});closesocket(client);return;
   }
   if(route=="/v1/click"){int x=input.value("x",-1),y=input.value("y",-1);RECT rect=windowBounds(target);if(x<rect.left||x>=rect.right||y<rect.top||y>=rect.bottom||std::abs(x)>20000||std::abs(y)>20000||!same(target)||!SetCursorPos(x,y)||!same(target))throw std::runtime_error("Click coordinates must be inside the approved foreground window");INPUT click[2]{};click[0].type=click[1].type=INPUT_MOUSE;click[0].mi.dwFlags=MOUSEEVENTF_LEFTDOWN;click[1].mi.dwFlags=MOUSEEVENTF_LEFTUP;if(SendInput(2,click,sizeof(INPUT))!=2){INPUT release{};release.type=INPUT_MOUSE;release.mi.dwFlags=MOUSEEVENTF_LEFTUP;SendInput(1,&release,sizeof(INPUT));throw std::runtime_error("Windows refused click input");}response(client,200,{{"ok",true},{"app",utf8(processName(target))},{"clicked",true}});closesocket(client);return;}
   if(route=="/v1/type"){auto text=input.value("text",std::string{});auto wideText=wide(text);if(wideText.size()>2000)throw std::runtime_error("Input exceeds 2000 UTF-16 code units");std::vector<INPUT> events;events.reserve(wideText.size()*2);for(wchar_t c:wideText){INPUT down{};down.type=INPUT_KEYBOARD;down.ki.wScan=WORD(c);down.ki.dwFlags=KEYEVENTF_UNICODE;events.push_back(down);INPUT up=down;up.ki.dwFlags|=KEYEVENTF_KEYUP;events.push_back(up);}if(!same(target))throw std::runtime_error("Foreground app changed; text was not sent");if(!events.empty()&&SendInput(UINT(events.size()),events.data(),sizeof(INPUT))!=events.size()){std::vector<INPUT> releases;releases.reserve(wideText.size());for(wchar_t c:wideText){INPUT up{};up.type=INPUT_KEYBOARD;up.ki.wScan=WORD(c);up.ki.dwFlags=KEYEVENTF_UNICODE|KEYEVENTF_KEYUP;releases.push_back(up);}SendInput(UINT(releases.size()),releases.data(),sizeof(INPUT));throw std::runtime_error("Windows refused the complete Unicode text input");}response(client,200,{{"ok",true},{"app",utf8(processName(target))},{"typed",true}});closesocket(client);return;}
   auto key=input.value("key",std::string{});auto events=keyboardChord(key);if(!same(target))throw std::runtime_error("Foreground app changed; key was not sent");if(SendInput(UINT(events.size()),events.data(),sizeof(INPUT))!=events.size()){std::vector<INPUT> releases;for(const auto& event:events)if(event.type==INPUT_KEYBOARD&&!(event.ki.dwFlags&KEYEVENTF_KEYUP)){INPUT up=event;up.ki.dwFlags=KEYEVENTF_KEYUP;releases.push_back(up);}if(!releases.empty())SendInput(UINT(releases.size()),releases.data(),sizeof(INPUT));throw std::runtime_error("Windows refused the complete key chord");}response(client,200,{{"ok",true},{"app",utf8(processName(target))},{"key",key}});closesocket(client);return;
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

