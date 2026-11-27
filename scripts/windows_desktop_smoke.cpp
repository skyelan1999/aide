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
static void capture(const wchar_t* name=L"native-desktop.bmp"){
 HWND hwnd=testApp->window;RedrawWindow(hwnd,nullptr,nullptr,RDW_INVALIDATE|RDW_FRAME|RDW_UPDATENOW|RDW_ALLCHILDREN);RECT bounds{};GetWindowRect(hwnd,&bounds);int width=bounds.right-bounds.left,height=bounds.bottom-bounds.top;
 HDC source=GetDC(nullptr),target=CreateCompatibleDC(source);BITMAPINFO info{};info.bmiHeader.biSize=sizeof(BITMAPINFOHEADER);info.bmiHeader.biWidth=width;info.bmiHeader.biHeight=-height;info.bmiHeader.biPlanes=1;info.bmiHeader.biBitCount=32;info.bmiHeader.biCompression=BI_RGB;void* pixels=nullptr;
 auto bitmap=CreateDIBSection(source,&info,DIB_RGB_COLORS,&pixels,nullptr,0);auto previous=SelectObject(target,bitmap);BitBlt(target,0,0,width,height,source,bounds.left,bounds.top,SRCCOPY|CAPTUREBLT);
 BITMAPFILEHEADER header{};header.bfType=0x4d42;header.bfOffBits=sizeof(header)+sizeof(info.bmiHeader);header.bfSize=header.bfOffBits+width*height*4;
 std::ofstream out(reportPath.parent_path()/name,std::ios::binary);out.write(reinterpret_cast<char*>(&header),sizeof(header));out.write(reinterpret_cast<char*>(&info.bmiHeader),sizeof(info.bmiHeader));out.write(static_cast<char*>(pixels),width*height*4);out.close();SelectObject(target,previous);DeleteObject(bitmap);DeleteDC(target);ReleaseDC(nullptr,source);
}
static void compactChromeChecks(){
 auto& app=*testApp;RECT bar{},viewport{};GetClientRect(app.bar,&bar);app.active()->controller->get_Bounds(&viewport);
 if(bar.bottom!=app.chrome.height()||viewport.top!=bar.bottom||app.chrome.height()!=MulDiv(32,int(app.chrome.dpi),96))throw std::runtime_error("Tab strip is not compact or overlaps web content");
 RECT selected{};TabCtrl_GetItemRect(app.bar,TabCtrl_GetCurSel(app.bar),&selected);RECT visible{};IntersectRect(&visible,&selected,&bar);if(visible.right-visible.left<(selected.right-selected.left)*.9)throw std::runtime_error("Selected tab clipped: dpi="+std::to_string(app.chrome.dpi)+" left="+std::to_string(selected.left)+" right="+std::to_string(selected.right)+" bar="+std::to_string(bar.right));
 int selectedIndex=TabCtrl_GetCurSel(app.bar);POINT closePoint{};RECT selectedClient{};TabCtrl_GetItemRect(app.bar,selectedIndex,&selectedClient);closePoint={selectedClient.right-app.chrome.px(13),(selectedClient.top+selectedClient.bottom)/2};if(!app.chrome.closeHit(app.bar,selectedIndex,closePoint))throw std::runtime_error("Per-tab close hit target missing");
 results["selectedTabRemainsVisible"]=true;results["perTabHoverCloseControl"]=true;
 if(auto scroll=aide_desktop::Chrome::scroller(app.bar)){RECT arrows{},button{},overlap{};GetWindowRect(scroll,&arrows);GetWindowRect(GetDlgItem(app.window,101),&button);if(IntersectRect(&overlap,&arrows,&button))throw std::runtime_error("New tab button overlaps native overflow arrows");}results["overflowButtonsUnobstructed"]=true;
 if(!SendMessageW(app.window,WM_GETICON,ICON_SMALL,0)||!SendMessageW(app.window,WM_GETICON,ICON_BIG,0))throw std::runtime_error("Window/taskbar icon missing");
 for(auto kind:{ICON_SMALL,ICON_BIG}){auto icon=reinterpret_cast<HICON>(SendMessageW(app.window,WM_GETICON,kind,0));ICONINFO info{};if(!GetIconInfo(icon,&info))throw std::runtime_error("Cannot read window icon bitmap");BITMAP bitmap{};GetObjectW(info.hbmColor,sizeof(bitmap),&bitmap);if(info.hbmColor)DeleteObject(info.hbmColor);if(info.hbmMask)DeleteObject(info.hbmMask);if(bitmap.bmWidth!=app.chrome.px(kind==ICON_SMALL?16:32))throw std::runtime_error("Window icon did not update for DPI");}
 HICON large=nullptr,small=nullptr;auto count=ExtractIconExW(module().c_str(),0,&large,&small,1);if(!count||count==UINT_MAX||!large||!small)throw std::runtime_error("Executable shell icon missing (count "+std::to_string(count)+", Windows error "+std::to_string(GetLastError())+")");results["shellIconExtractionCount"]=count;DestroyIcon(large);DestroyIcon(small);
 if(!AreDpiAwarenessContextsEqual(GetWindowDpiAwarenessContext(app.window),DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2))throw std::runtime_error("Per-monitor-v2 resource manifest missing");
 if(aide_desktop::Chrome::contrast(aide_desktop::Chrome::readable(app.chrome.muted,app.chrome.background),app.chrome.background)<4.5)throw std::runtime_error("Inactive title contrast below 4.5");
 results["compact32DipStrip"]=true;results["windowAndShellIcons"]=true;results["perMonitorV2Manifest"]=true;results["inactiveTitleContrast"]=true;
}
static void layoutCaptures(bool dark){
 auto& app=*testApp;RECT original{};GetWindowRect(app.window,&original);auto dpi=app.chrome.dpi;
 SetWindowPos(app.window,nullptr,original.left,original.top,640,480,SWP_NOZORDER|SWP_NOACTIVATE);
 int before=TabCtrl_GetItemCount(app.bar);for(int i=before;i<10;i++){TCITEMW item{};item.mask=TCIF_TEXT;item.pszText=const_cast<wchar_t*>(L"项目终端 · Device Terminal");TabCtrl_InsertItem(app.bar,i,&item);}app.resize();TabCtrl_SetCurSel(app.bar,9);app.resize();
 RedrawWindow(app.window,nullptr,nullptr,RDW_INVALIDATE|RDW_UPDATENOW|RDW_ALLCHILDREN);capture(dark?L"compact-dark-narrow.bmp":L"compact-light-narrow.bmp");
 // Synthetic monitor message exercises the same DPI handler on the runner's
 // single display. It does not claim physical multi-monitor hardware coverage.
 RECT narrow{};GetWindowRect(app.window,&narrow);SendMessageW(app.window,WM_DPICHANGED,MAKEWPARAM(144,144),reinterpret_cast<LPARAM>(&narrow));compactChromeChecks();
 RedrawWindow(app.window,nullptr,nullptr,RDW_INVALIDATE|RDW_UPDATENOW|RDW_ALLCHILDREN);capture(dark?L"compact-dark-150.bmp":L"compact-light-150.bmp");
 SendMessageW(app.window,WM_DPICHANGED,MAKEWPARAM(dpi,dpi),reinterpret_cast<LPARAM>(&original));for(int i=9;i>=before;i--)TabCtrl_DeleteItem(app.bar,i);TabCtrl_SetCurSel(app.bar,int(app.selected));app.resize();results["narrowOverflowAndSyntheticDpi"]=true;
}
static void pollScript(const std::wstring& script,std::function<void(const json&)> done){
 if(waiting||!testApp->active()||!testApp->active()->web)return;waiting=true;
 auto callback=aide_desktop::callback<ICoreWebView2ExecuteScriptCompletedHandler,HRESULT,LPCWSTR>([done](HRESULT code,LPCWSTR value){waiting=false;if(finished)return S_OK;try{if(SUCCEEDED(code)&&value){auto v=json::parse(utf8(value),nullptr,false);if(!v.is_discarded())done(v);}}catch(const std::exception& e){finish(false,e.what());}return S_OK;});
 if(FAILED(testApp->active()->web->ExecuteScript(script.c_str(),callback.get()))){waiting=false;finish(false,"Script evaluation failed");}
}
static void clickCloseTab(int index){RECT r{};if(!TabCtrl_GetItemRect(testApp->bar,index,&r))throw std::runtime_error("Cannot locate tab close target");int x=int(r.right)-testApp->chrome.px(13),y=(int(r.top)+int(r.bottom))/2;LPARAM point=MAKELPARAM(short(x),short(y));SendMessageW(testApp->bar,WM_MOUSEMOVE,0,point);SendMessageW(testApp->bar,WM_LBUTTONDOWN,MK_LBUTTON,point);SendMessageW(testApp->bar,WM_LBUTTONUP,0,point);}
static VOID CALLBACK tick(HWND,UINT,UINT_PTR,DWORD){
 try{
 if(finished||!testApp||!testApp->window)return;
 if(GetTickCount()-began>90000){finish(false,"Timed out in phase "+std::to_string(phase));return;}
 if(phase==0){pollScript(L"({ready:window.fixtureReady===true,native:window.fixtureNativeReady===true})",[](const json& v){if(!v.is_object()||!v.value("ready",false)||!v.value("native",false))return;RECT work{},bounds{};SystemParametersInfoW(SPI_GETWORKAREA,0,&work,0);GetWindowRect(testApp->window,&bounds);if(bounds.left<work.left||bounds.top<work.top||bounds.right>work.right||bounds.bottom>work.bottom)throw std::runtime_error("Desktop default window exceeds work area");compactChromeChecks();results["windowFitsWorkArea"]=true;results["localTLSPin"]=true;results["nativeHelloProbe"]=true;phase=1;testApp->active()->web->ExecuteScript(L"document.getElementById('open').click();",nullptr);});}
 else if(phase==1&&testApp->tabs.size()==2){pollScript(L"({ready:window.fixturePluginReady===true,blocked:window.fixtureNativeBlocked===true,title:document.title})",[](const json& v){if(!v.is_object()||!v.value("ready",false))return;if(!v.value("blocked",false))throw std::runtime_error("Plugin page exposed native authentication");results["appTabs"]=testApp->tabs.size()==2;results["sharedStorageSandboxPort"]=true;results["pluginNativeBridgeBlocked"]=true;phase=2;capture();clickCloseTab(int(testApp->selected));});}
 else if(phase==2&&testApp->tabs.size()==1){results["closePluginKeepsMain"]=true;results["tabCloseClickWorks"]=true;phase=19;testApp->add(testApp->entry);}
 else if(phase==19&&testApp->tabs.size()==2){pollScript(L"({ready:window.fixtureReady===true})",[](const json& v){if(!v.is_object()||!v.value("ready",false))return;phase=18;clickCloseTab(0);});}
 else if(phase==18&&testApp->tabs.size()==1&&testApp->selected==0){results["closingInactiveTabPreservesActivePage"]=true;phase=20;testApp->active()->web->ExecuteScript(L"document.getElementById('theme').click()",nullptr);}
 else if(phase==20&&!testApp->chrome.dark&&testApp->chrome.selected==RGB(226,232,240)){results["nativeLightPalette"]=true;compactChromeChecks();capture(L"native-light.bmp");layoutCaptures(false);phase=21;testApp->active()->web->ExecuteScript(L"document.getElementById('theme').click()",nullptr);}
 else if(phase==21&&testApp->chrome.dark&&testApp->chrome.selected==RGB(52,66,82)){results["nativeDarkPalette"]=true;compactChromeChecks();capture(L"compact-dark.bmp");layoutCaptures(true);phase=3;phaseStarted=GetTickCount();pollScript(L"window.AidePluginWorkbench={isDirty:()=>true};true",[](const json&){testApp->close(testApp->active());});}
 else if(phase==3){HWND dialog=FindWindowW(L"#32770",L"Aide");if(dialog){PostMessageW(dialog,WM_COMMAND,IDNO,0);phase=4;phaseStarted=GetTickCount();}}
 else if(phase==4&&GetTickCount()-phaseStarted>600){if(testApp->tabs.size()!=1||testApp->closingAll)finish(false,"Cancelled dirty close lost main tab");else{results["dirtyCloseCancelPreservesTab"]=true;phase=5;phaseStarted=GetTickCount();pollScript(L"window.AidePluginWorkbench={isDirty:()=>false};const a=document.createElement('a');a.href=URL.createObjectURL(new Blob(['fixture']));a.download='fixture.txt';a.click();true",[](const json&){});}}
 else if(phase==5){HWND dialog=FindWindowW(L"#32770",nullptr);if(dialog){PostMessageW(dialog,WM_COMMAND,IDCANCEL,0);phase=6;phaseStarted=GetTickCount();}}
 else if(phase==6&&GetTickCount()-phaseStarted>800){results["downloadDialogCancel"]=true;finish(true);}
 }catch(const std::exception& e){capture(L"compact-failure.bmp");finish(false,e.what());}
}
int wmain(int argc,wchar_t** argv){
 try{if(argc!=5)throw std::runtime_error("Expected readiness JSON, private profile, loader DLL, result JSON");
  auto ready=parseJson(read(argv[1],65536));auto origin=wide(ready.at("origin").get<std::string>());if(!std::regex_match(origin,std::wregex(L"https://localhost:[0-9]{1,5}")))throw std::runtime_error("Fixture origin invalid");
  fs::path profile=argv[2];privateDirectory(profile);reportPath=argv[4];Desktop app(origin,origin+L"/",profile,L"fixture_"+wide(sha256(argv[1])),read(wide(ready.at("certificate").get<std::string>()),65536),std::string{});testApp=&app;began=GetTickCount();SetTimer(nullptr,0,200,tick);int code=app.run(argv[3]);if(!finished)finish(false,"Desktop closed before checks completed");std::cout<<results.dump(2)<<'\n';return results.value("ok",false)?code:1;
 }catch(const std::exception& e){std::cerr<<e.what()<<'\n';return 1;}
}
