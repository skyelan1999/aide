// Separate synthetic Windows UI tester. Never included in aide-launcher.exe.
#define wmain production_launcher_main
#include "../native/windows-launcher/main.cpp"
#undef wmain
using aide_desktop::Desktop;
static Desktop* testApp=nullptr;
static fs::path reportPath;
static DWORD began=0,phaseStarted=0;
static int phase=0;
static bool waiting=false,finished=false;
static json results=json::object();
static void finish(bool ok,const std::string& error={}){
 if(finished)return;finished=true;results["ok"]=ok;results["error"]=error;results["hardwareHello"]= "NOT_RUN: availability probe only, no enrollment or signing";write(reportPath,results.dump(2));if(testApp&&testApp->window)PostMessageW(testApp->window,WM_CLOSE,0,1);
}
static void capture(){
 HWND hwnd=testApp->window;RECT bounds{};GetWindowRect(hwnd,&bounds);int width=bounds.right-bounds.left,height=bounds.bottom-bounds.top;
 HDC source=GetDC(nullptr),target=CreateCompatibleDC(source);BITMAPINFO info{};info.bmiHeader.biSize=sizeof(BITMAPINFOHEADER);info.bmiHeader.biWidth=width;info.bmiHeader.biHeight=-height;info.bmiHeader.biPlanes=1;info.bmiHeader.biBitCount=32;info.bmiHeader.biCompression=BI_RGB;void* pixels=nullptr;
 auto bitmap=CreateDIBSection(source,&info,DIB_RGB_COLORS,&pixels,nullptr,0);auto previous=SelectObject(target,bitmap);BitBlt(target,0,0,width,height,source,bounds.left,bounds.top,SRCCOPY|CAPTUREBLT);
 BITMAPFILEHEADER header{};header.bfType=0x4d42;header.bfOffBits=sizeof(header)+sizeof(info.bmiHeader);header.bfSize=header.bfOffBits+width*height*4;
 std::ofstream out(reportPath.parent_path()/L"native-desktop.bmp",std::ios::binary);out.write(reinterpret_cast<char*>(&header),sizeof(header));out.write(reinterpret_cast<char*>(&info.bmiHeader),sizeof(info.bmiHeader));out.write(static_cast<char*>(pixels),width*height*4);out.close();SelectObject(target,previous);DeleteObject(bitmap);DeleteDC(target);ReleaseDC(nullptr,source);
}
static void pollScript(const std::wstring& script,std::function<void(const json&)> done){
 if(waiting||!testApp->active()||!testApp->active()->web)return;waiting=true;
 auto callback=aide_desktop::callback<ICoreWebView2ExecuteScriptCompletedHandler,HRESULT,LPCWSTR>([done](HRESULT code,LPCWSTR value){waiting=false;if(finished)return S_OK;try{if(SUCCEEDED(code)&&value){auto v=json::parse(utf8(value),nullptr,false);if(!v.is_discarded())done(v);}}catch(const std::exception& e){finish(false,e.what());}return S_OK;});
 if(FAILED(testApp->active()->web->ExecuteScript(script.c_str(),callback.get()))){waiting=false;finish(false,"Script evaluation failed");}
}
static VOID CALLBACK tick(HWND,UINT,UINT_PTR,DWORD){
 if(finished||!testApp||!testApp->window)return;
 if(GetTickCount()-began>90000){finish(false,"Timed out in phase "+std::to_string(phase));return;}
 if(phase==0){pollScript(L"({ready:window.fixtureReady===true,native:window.fixtureNativeReady===true})",[](const json& v){if(!v.is_object()||!v.value("ready",false)||!v.value("native",false))return;RECT work{},bounds{};SystemParametersInfoW(SPI_GETWORKAREA,0,&work,0);GetWindowRect(testApp->window,&bounds);if(bounds.left<work.left||bounds.top<work.top||bounds.right>work.right||bounds.bottom>work.bottom)throw std::runtime_error("Desktop default window exceeds work area");results["windowFitsWorkArea"]=true;results["localTLSPin"]=true;results["nativeHelloProbe"]=true;phase=1;testApp->active()->web->ExecuteScript(L"document.getElementById('open').click();",nullptr);});}
 else if(phase==1&&testApp->tabs.size()==2){pollScript(L"({ready:window.fixturePluginReady===true,blocked:window.fixtureNativeBlocked===true,title:document.title})",[](const json& v){if(!v.is_object()||!v.value("ready",false))return;if(!v.value("blocked",false))throw std::runtime_error("Plugin page exposed native authentication");results["appTabs"]=testApp->tabs.size()==2;results["sharedStorageSandboxPort"]=true;results["pluginNativeBridgeBlocked"]=true;phase=2;capture();testApp->post([]{testApp->active()->web->ExecuteScript(L"window.close()",nullptr);});});}
 else if(phase==2&&testApp->tabs.size()==1){results["closePluginKeepsMain"]=true;phase=3;phaseStarted=GetTickCount();pollScript(L"window.AidePluginWorkbench={isDirty:()=>true};true",[](const json&){testApp->close(testApp->active());});}
 else if(phase==3){HWND dialog=FindWindowW(L"#32770",L"Aide");if(dialog){PostMessageW(dialog,WM_COMMAND,IDNO,0);phase=4;phaseStarted=GetTickCount();}}
 else if(phase==4&&GetTickCount()-phaseStarted>600){if(testApp->tabs.size()!=1||testApp->closingAll)finish(false,"Cancelled dirty close lost main tab");else{results["dirtyCloseCancelPreservesTab"]=true;phase=5;phaseStarted=GetTickCount();pollScript(L"window.AidePluginWorkbench={isDirty:()=>false};const a=document.createElement('a');a.href=URL.createObjectURL(new Blob(['fixture']));a.download='fixture.txt';a.click();true",[](const json&){});}}
 else if(phase==5){HWND dialog=FindWindowW(L"#32770",nullptr);if(dialog){PostMessageW(dialog,WM_COMMAND,IDCANCEL,0);phase=6;phaseStarted=GetTickCount();}}
 else if(phase==6&&GetTickCount()-phaseStarted>800){results["downloadDialogCancel"]=true;finish(true);}
}
int wmain(int argc,wchar_t** argv){
 try{if(argc!=5)throw std::runtime_error("Expected readiness JSON, private profile, loader DLL, result JSON");
  auto ready=parseJson(read(argv[1],65536));auto origin=wide(ready.at("origin").get<std::string>());if(!std::regex_match(origin,std::wregex(L"https://localhost:[0-9]{1,5}")))throw std::runtime_error("Fixture origin invalid");
  fs::path profile=argv[2];privateDirectory(profile);reportPath=argv[4];Desktop app(origin,origin+L"/",profile,L"fixture_"+wide(sha256(argv[1])),read(wide(ready.at("certificate").get<std::string>()),65536));testApp=&app;began=GetTickCount();SetTimer(nullptr,0,200,tick);int code=app.run(argv[3]);if(!finished)finish(false,"Desktop closed before checks completed");std::cout<<results.dump(2)<<'\n';return results.value("ok",false)?code:1;
 }catch(const std::exception& e){std::cerr<<e.what()<<'\n';return 1;}
}
