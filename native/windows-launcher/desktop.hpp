#pragma once
#include <commctrl.h>
#include <wincrypt.h>
#include <functional>
#include "WebView2.h"
#include "desktop_policy.hpp"
#include "desktop_com.hpp"
#include "desktop_hello.hpp"
#include "desktop_chrome.hpp"
#include "computer_bridge.hpp"
namespace aide_desktop {
inline void checked(HRESULT result,const char* message){if(FAILED(result))throw std::runtime_error(message);}
inline std::vector<BYTE> certificate(const std::string& pem) {
 DWORD count=0;if(pem.size()>65536||!CryptStringToBinaryA(pem.c_str(),DWORD(pem.size()),CRYPT_STRING_BASE64HEADER,nullptr,&count,nullptr,nullptr))throw std::runtime_error("Local certificate unavailable");
 std::vector<BYTE> bytes(count);if(!CryptStringToBinaryA(pem.c_str(),DWORD(pem.size()),CRYPT_STRING_BASE64HEADER,bytes.data(),&count,nullptr,nullptr))throw std::runtime_error("Invalid local certificate");
 auto cert=CertCreateCertificateContext(X509_ASN_ENCODING,bytes.data(),count);if(!cert)throw std::runtime_error("Invalid local certificate");
 wchar_t name[512]{};auto size=CertGetNameStringW(cert,CERT_NAME_DNS_TYPE,0,nullptr,name,512);bool valid=CertVerifyTimeValidity(nullptr,cert->pCertInfo)==0&&size>1&&_wcsicmp(name,L"localhost")==0;CertFreeCertificateContext(cert);
 if(!valid)throw std::runtime_error("Local certificate host or validity mismatch");return bytes;
}
struct Desktop {
 struct Tab {Ptr<ICoreWebView2Controller> controller;Ptr<ICoreWebView2> web;bool closed=false,closing=false;unsigned generation=0;std::wstring title=L"Aide";};
 struct Reply {std::shared_ptr<Tab> tab;unsigned generation;std::string id;json value;std::shared_ptr<Hello::Operation> operation;};
 HWND window=nullptr,bar=nullptr;std::wstring origin,entry,className;fs::path profile,statusFile;std::vector<BYTE> pin;
 Ptr<ICoreWebView2Environment> environment;std::vector<std::shared_ptr<Tab>> tabs;
 Chrome chrome;Hello hello;std::shared_ptr<Hello::Operation> ceremony;size_t selected=0;bool closingAll=false;DWORD closeStarted=0;
 ComputerBridge computer;
 HMODULE loader=nullptr;HICON smallIcon=nullptr,bigIcon=nullptr;
 ~Desktop(){if(smallIcon)DestroyIcon(smallIcon);if(bigIcon)DestroyIcon(bigIcon);}
 void icons(){auto instance=GetModuleHandleW(nullptr);auto small=static_cast<HICON>(LoadImageW(instance,MAKEINTRESOURCEW(201),IMAGE_ICON,chrome.px(16),chrome.px(16),0)),big=static_cast<HICON>(LoadImageW(instance,MAKEINTRESOURCEW(201),IMAGE_ICON,chrome.px(32),chrome.px(32),0));if(small){SendMessageW(window,WM_SETICON,ICON_SMALL,reinterpret_cast<LPARAM>(small));if(smallIcon)DestroyIcon(smallIcon);smallIcon=small;}if(big){SendMessageW(window,WM_SETICON,ICON_BIG,reinterpret_cast<LPARAM>(big));if(bigIcon)DestroyIcon(bigIcon);bigIcon=big;}}
 Desktop(std::wstring address,std::wstring initial,fs::path directory,std::wstring identity,std::string pem,std::string computerToken,fs::path status={}):origin(std::move(address)),entry(std::move(initial)),className(L"AideDesktop_"+identity),profile(std::move(directory)),statusFile(std::move(status)),pin(certificate(pem)),computer(std::move(computerToken)){}
 void post(std::function<void()> fn){auto task=new std::function<void()>(std::move(fn));if(!PostMessageW(window,WM_APP+2,0,reinterpret_cast<LPARAM>(task)))delete task;}
 void fail(const wchar_t* text){std::wstring message=text;post([this,message]{MessageBoxW(window,message.c_str(),L"Aide",MB_OK|MB_ICONERROR);if(tabs.empty())DestroyWindow(window);});}
 std::shared_ptr<Tab> active(){return selected<tabs.size()?tabs[selected]:nullptr;}
 void pause(const std::shared_ptr<Tab>& tab){if(tab&&tab->web)tab->web->ExecuteScript(L"window.aideAssistantCall?.end?.();window.aideAssistantLive?.stop?.();if(typeof voiceClose==='function')voiceClose();if(typeof ttsCancel==='function')ttsCancel();document.querySelectorAll('audio,video').forEach(x=>x.pause());window.speechSynthesis?.cancel();",nullptr);}
 void resize(){if(!window)return;RECT bounds{};GetClientRect(window,&bounds);int h=chrome.height(),button=chrome.px(28),gap=chrome.px(4);int count=bar?std::max(1,TabCtrl_GetItemCount(bar)):1;int available=std::max(0,int(bounds.right)-button-gap);bounds.top=h;
  for(auto& t:tabs)if(t->controller)t->controller->put_Bounds(bounds);
  if(bar){MoveWindow(bar,0,0,available,h,TRUE);chrome.layout(bar,available);}
  RECT last{};int newX=available+gap;if(bar&&!Chrome::scroller(bar)&&TabCtrl_GetItemRect(bar,count-1,&last)&&last.right+gap+button<bounds.right)newX=std::max(gap,int(last.right)+gap);MoveWindow(GetDlgItem(window,101),newX,chrome.px(2),button,button,TRUE);InvalidateRect(window,nullptr,FALSE);
 }

 void select(size_t index){if(index>=tabs.size())return;if(active()!=tabs[index]){pause(active());hello.cancel(ceremony);}selected=index;TabCtrl_SetCurSel(bar,int(index));for(size_t i=0;i<tabs.size();i++)if(tabs[i]->controller)tabs[i]->controller->put_IsVisible(i==index);if(active()->controller)active()->controller->MoveFocus(COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC);SetWindowTextW(window,active()->title.c_str());resize();}
 void erase(const std::shared_ptr<Tab>& tab){if(tab->closed)return;tab->closed=true;pause(tab);hello.cancel(ceremony);if(tab->controller)tab->controller->Close();tab->web=Ptr<ICoreWebView2>();tab->controller=Ptr<ICoreWebView2Controller>();auto i=std::find(tabs.begin(),tabs.end(),tab);if(i==tabs.end())return;size_t index=size_t(i-tabs.begin());bool wasSelected=index==selected;TabCtrl_DeleteItem(bar,int(index));tabs.erase(i);if(tabs.empty()){DestroyWindow(window);return;}if(index<selected)--selected;if(wasSelected)selected=std::min(index,tabs.size()-1);select(selected);}
 void close(const std::shared_ptr<Tab>& tab){if(!tab||tab->closed||tab->closing)return;tab->closing=true;
  if(!tab->web){erase(tab);return;}
  auto done=callback<ICoreWebView2ExecuteScriptCompletedHandler,HRESULT,LPCWSTR>([this,tab](HRESULT result,LPCWSTR value){tab->closing=false;if(tab->closed)return S_OK;
   bool uncertain=FAILED(result)||!value;bool dirty=value&&std::wstring(value)==L"true";
   post([this,tab,uncertain,dirty]{if(tab->closed)return;
   if((uncertain||dirty)&&MessageBoxW(window,L"This page may contain unsaved edits. Close it?",L"Aide",MB_YESNO|MB_ICONWARNING|MB_DEFBUTTON2)!=IDYES){closingAll=false;return;}
   erase(tab);if(closingAll&&!tabs.empty())close(tabs.back());});return S_OK;});
  if(FAILED(tab->web->ExecuteScript(L"window.AidePluginWorkbench?.isDirty?.()===true",done.get()))){tab->closing=false;closingAll=false;MessageBoxW(window,L"The page did not confirm its state. Please retry closing.",L"Aide",MB_OK|MB_ICONWARNING);}
 }
 void send(const std::shared_ptr<Tab>& tab,const std::string& id,const json& value){if(!tab->closed&&tab->web){auto reply=json{{"aideBiometrics",true},{"id",id},{"result",value}}.dump();tab->web->PostWebMessageAsJson(wide(reply).c_str());}}
 void message(const std::shared_ptr<Tab>& tab,ICoreWebView2WebMessageReceivedEventArgs* args){
  LPWSTR sourceRaw=nullptr,bodyRaw=nullptr;checked(args->get_Source(&sourceRaw),"Message source unavailable");auto source=takeString(sourceRaw);
  if(!authRoot(source,origin)||tab!=active()||tab->closed||!IsWindowVisible(window)||IsIconic(window))return;
  if(FAILED(args->get_WebMessageAsJson(&bodyRaw)))return;auto text=utf8(takeString(bodyRaw));if(text.size()>65536)return;auto body=json::parse(text,nullptr,false);
  if(!body.is_object()||body.size()!=3||!body.contains("id")||!body["id"].is_string()||!body.contains("operation")||!body["operation"].is_string()||!body.contains("options")||!body["options"].is_object())return;
  auto id=body["id"].get<std::string>(),operation=body["operation"].get<std::string>();if(!std::regex_match(id,std::regex("[0-9]{1,9}")))return;
  if(operation=="appearance"){try{chrome.apply(window,bar,body["options"]);}catch(...){}return;}
  auto reject=[&](const char* name,const char* message){send(tab,id,{{"error",{{"name",name},{"message",message}}}});};
  if(operation=="status"){send(tab,id,hello.status(profile/L"hello-credentials.json"));return;}
  if(operation!="create"&&operation!="get"){reject("SecurityError","Unsupported native operation");return;}
  if(ceremony){reject("OperationError","Windows Hello is busy");return;}
  try{ceremony=hello.begin();}catch(...){reject("NotSupportedError","Windows Hello is unavailable; use the account password");return;}
  auto op=ceremony;auto options=body["options"];auto address=utf8(origin);auto file=profile/L"hello-credentials.json";auto owner=window;auto generation=tab->generation;auto api=hello;
  std::thread([api,op]{for(int i=0;i<1250&&!op->done;i++){if(op->cancelled)api.cancel(op);Sleep(100);}if(!op->done)api.cancel(op);}).detach();
  std::thread([api,op,options,operation,address,file,owner,tab,generation,id]()mutable{
   json result;try{result=api.perform(owner,operation,options,address,file,op);}catch(...){result={{"error",{{"name",(op->cancelled||op->userCancelled)?"NotAllowedError":"OperationError"},{"message","Windows Hello cancelled or failed; use the account password or bind it in Settings > Account"}}}};}
   op->done=true;auto reply=new Reply{tab,generation,id,std::move(result),op};if(!PostMessageW(owner,WM_APP+1,0,reinterpret_cast<LPARAM>(reply)))delete reply;
  }).detach();
 }
 void configure(const std::shared_ptr<Tab>& tab,std::function<void(bool)> ready){
  EventRegistrationToken acceleratorToken{};
  auto accelerator=callback<ICoreWebView2AcceleratorKeyPressedEventHandler,ICoreWebView2Controller*,ICoreWebView2AcceleratorKeyPressedEventArgs*>([this](ICoreWebView2Controller*,auto* args){
   COREWEBVIEW2_KEY_EVENT_KIND kind;UINT key=0;args->get_KeyEventKind(&kind);args->get_VirtualKey(&key);
   if(kind!=COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN)return S_OK;bool ctrl=(GetKeyState(VK_CONTROL)&0x8000)!=0;UINT command=0;
   if(ctrl&&key=='T')command=101;else if(ctrl&&key=='W')command=102;else if(key==VK_F5||(ctrl&&key=='R'))command=103;else if(ctrl&&key==VK_TAB)command=(GetKeyState(VK_SHIFT)&0x8000)?105:104;else if(ctrl&&key==VK_OEM_COMMA)command=106;
   if(command){args->put_Handled(TRUE);PostMessageW(window,WM_COMMAND,command,0);}return S_OK;});
  checked(tab->controller->add_AcceleratorKeyPressed(accelerator.get(),&acceleratorToken),"Desktop shortcuts unavailable");
  Ptr<ICoreWebView2Settings> settings;checked(tab->web->get_Settings(settings.put()),"WebView settings unavailable");settings->put_AreDefaultScriptDialogsEnabled(TRUE);settings->put_IsStatusBarEnabled(FALSE);settings->put_AreHostObjectsAllowed(FALSE);settings->put_AreDevToolsEnabled(FALSE);
  EventRegistrationToken token{};
  auto navigation=callback<ICoreWebView2NavigationStartingEventHandler,ICoreWebView2*,ICoreWebView2NavigationStartingEventArgs*>([this,tab](ICoreWebView2*,auto* args){
   LPWSTR raw=nullptr;args->get_Uri(&raw);auto uri=takeString(raw);BOOL user=FALSE;args->get_IsUserInitiated(&user);
   ++tab->generation;if(tab==active())hello.cancel(ceremony);
   if(!local(uri,origin)){args->put_Cancel(TRUE);if(user&&external(uri))ShellExecuteW(window,L"open",uri.c_str(),nullptr,nullptr,SW_SHOWNORMAL);}return S_OK;});
  checked(tab->web->add_NavigationStarting(navigation.get(),&token),"Navigation guard unavailable");
  auto title=callback<ICoreWebView2DocumentTitleChangedEventHandler,ICoreWebView2*,IUnknown*>([this,tab](ICoreWebView2* web,IUnknown*){
   if(tab->closed)return S_OK;LPWSTR raw=nullptr;web->get_DocumentTitle(&raw);tab->title=takeString(raw).substr(0,160);auto i=std::find(tabs.begin(),tabs.end(),tab);if(i!=tabs.end()){TCITEMW item{};item.mask=TCIF_TEXT;item.pszText=tab->title.data();TabCtrl_SetItem(bar,int(i-tabs.begin()),&item);}if(tab==active())SetWindowTextW(window,tab->title.c_str());return S_OK;});
  tab->web->add_DocumentTitleChanged(title.get(),&token);
  auto popup=callback<ICoreWebView2NewWindowRequestedEventHandler,ICoreWebView2*,ICoreWebView2NewWindowRequestedEventArgs*>([this,tab](ICoreWebView2*,auto* args){
   LPWSTR raw=nullptr;args->get_Uri(&raw);auto uri=takeString(raw);args->put_Handled(TRUE);BOOL user=FALSE;args->get_IsUserInitiated(&user);
   if(tab->closed||!local(uri,origin)){if(user&&external(uri))ShellExecuteW(window,L"open",uri.c_str(),nullptr,nullptr,SW_SHOWNORMAL);return S_OK;}
   Ptr<ICoreWebView2Deferral> deferral;checked(args->GetDeferral(deferral.put()),"Popup deferral unavailable");Ptr<ICoreWebView2NewWindowRequestedEventArgs> pending(args);
   add(L"",[pending,deferral](const std::shared_ptr<Tab>& created){if(created&&created->web)pending->put_NewWindow(created->web.get());deferral->Complete();});return S_OK;});
  checked(tab->web->add_NewWindowRequested(popup.get(),&token),"App tabs unavailable");
  auto closeEvent=callback<ICoreWebView2WindowCloseRequestedEventHandler,ICoreWebView2*,IUnknown*>([this,tab](ICoreWebView2*,IUnknown*){close(tab);return S_OK;});tab->web->add_WindowCloseRequested(closeEvent.get(),&token);
  auto permissions=callback<ICoreWebView2PermissionRequestedEventHandler,ICoreWebView2*,ICoreWebView2PermissionRequestedEventArgs*>([this,tab](ICoreWebView2*,auto* args){
   LPWSTR raw=nullptr;args->get_Uri(&raw);auto uri=takeString(raw);COREWEBVIEW2_PERMISSION_KIND kind;args->get_PermissionKind(&kind);
   if(!authRoot(uri,origin)||tab!=active()){args->put_State(COREWEBVIEW2_PERMISSION_STATE_DENY);return S_OK;}
   if(kind==COREWEBVIEW2_PERMISSION_KIND_MICROPHONE||kind==COREWEBVIEW2_PERMISSION_KIND_CAMERA){Ptr<ICoreWebView2Deferral> defer;if(FAILED(args->GetDeferral(defer.put()))){args->put_State(COREWEBVIEW2_PERMISSION_STATE_DENY);return S_OK;}Ptr<ICoreWebView2PermissionRequestedEventArgs> pending(args);post([this,tab,kind,pending,defer]{bool allow=!tab->closed&&tab==active()&&MessageBoxW(window,kind==COREWEBVIEW2_PERMISSION_KIND_MICROPHONE?L"Allow this Aide page to use the microphone?":L"Allow this Aide page to use the camera?",L"Aide",MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2)==IDYES;pending->put_State(allow?COREWEBVIEW2_PERMISSION_STATE_ALLOW:COREWEBVIEW2_PERMISSION_STATE_DENY);defer->Complete();});}return S_OK;});
  tab->web->add_PermissionRequested(permissions.get(),&token);
  auto messages=callback<ICoreWebView2WebMessageReceivedEventHandler,ICoreWebView2*,ICoreWebView2WebMessageReceivedEventArgs*>([this,tab](ICoreWebView2*,auto* args){message(tab,args);return S_OK;});tab->web->add_WebMessageReceived(messages.get(),&token);
  const wchar_t* script=LR"JS((()=>{if(window!==window.top||location.pathname!=='/'||location.search||new URLSearchParams(location.hash.slice(1)).has('pluginWorkbench')||new URLSearchParams(location.hash.slice(1)).has('file'))return;let sequence=0;const pending=new Map();const appearance=()=>{const s=getComputedStyle(document.documentElement),hex=(key,fallback)=>{const v=s.getPropertyValue(key).trim();return /^#[0-9a-f]{6}$/i.test(v)?v:fallback;};chrome.webview.postMessage({id:'0',operation:'appearance',options:{dark:document.documentElement.dataset.theme!=='light',background:hex('--bg','#202328'),surface:hex('--panel','#24282e'),selected:hex('--selected','#344252'),text:hex('--text','#e0e4eb'),muted:hex('--muted','#a4adb9'),line:hex('--line','#373c44')}});};addEventListener('DOMContentLoaded',()=>{appearance();new MutationObserver(appearance).observe(document.documentElement,{attributes:true,attributeFilter:['data-theme','data-palette']});},{once:true});chrome.webview.addEventListener('message',event=>{const m=event.data;if(!m?.aideBiometrics)return;const p=pending.get(m.id);if(!p)return;pending.delete(m.id);clearTimeout(p.timer);if(m.result?.error)p.reject(new DOMException(m.result.error.message,m.result.error.name));else p.resolve(m.result);});const request=(operation,options={})=>new Promise((resolve,reject)=>{const id=String(++sequence),timer=setTimeout(()=>{pending.delete(id);reject(new DOMException('Windows Hello request expired','NotAllowedError'));},125000);pending.set(id,{resolve,reject,timer});chrome.webview.postMessage({id,operation,options});});Object.defineProperty(window,'AideNativeBiometrics',{value:Object.freeze({platform:'windows',status:()=>request('status'),create:o=>request('create',o),get:o=>request('get',o)}),writable:false,configurable:false});})();)JS";

  auto security=tab->web.as<ICoreWebView2_14>();if(!security)throw std::runtime_error("Update Microsoft Edge WebView2 Runtime to enable local TLS validation");
  auto cert=callback<ICoreWebView2ServerCertificateErrorDetectedEventHandler,ICoreWebView2*,ICoreWebView2ServerCertificateErrorDetectedEventArgs*>([this](ICoreWebView2*,auto* args){
   args->put_Action(COREWEBVIEW2_SERVER_CERTIFICATE_ERROR_ACTION_CANCEL);LPWSTR raw=nullptr;args->get_RequestUri(&raw);auto uri=takeString(raw);if(!local(uri,origin)||uri.rfind(L"https:",0)!=0)return S_OK;
   Ptr<ICoreWebView2Certificate> leaf;if(FAILED(args->get_ServerCertificate(leaf.put()))||!leaf)return S_OK;LPWSTR pem=nullptr;if(FAILED(leaf->ToPemEncoding(&pem)))return S_OK;
   try{if(certificate(utf8(takeString(pem)))==pin)args->put_Action(COREWEBVIEW2_SERVER_CERTIFICATE_ERROR_ACTION_ALWAYS_ALLOW);}catch(...){}return S_OK;});
  checked(security->add_ServerCertificateErrorDetected(cert.get(),&token),"TLS pin event unavailable");
  auto downloads=tab->web.as<ICoreWebView2_4>();if(!downloads)throw std::runtime_error("WebView2 download support unavailable");
  auto download=callback<ICoreWebView2DownloadStartingEventHandler,ICoreWebView2*,ICoreWebView2DownloadStartingEventArgs*>([this,tab](ICoreWebView2*,auto* args){
   LPWSTR source=nullptr;tab->web->get_Source(&source);if(tab->closed||!local(takeString(source),origin)){args->put_Cancel(TRUE);return S_OK;}
   Ptr<ICoreWebView2Deferral> defer;if(FAILED(args->GetDeferral(defer.put()))){args->put_Cancel(TRUE);return S_OK;}Ptr<ICoreWebView2DownloadStartingEventArgs> pending(args);
   post([this,tab,pending,defer]{
   struct Done{Ptr<ICoreWebView2Deferral> value;~Done(){value->Complete();}} done{defer};
   if(tab->closed){pending->put_Cancel(TRUE);return;}
   LPWSTR current=nullptr;pending->get_ResultFilePath(&current);auto name=fs::path(takeString(current)).filename().wstring();Ptr<IFileSaveDialog> dialog;
   if(FAILED(CoCreateInstance(CLSID_FileSaveDialog,nullptr,CLSCTX_INPROC_SERVER,IID_PPV_ARGS(dialog.put())))){pending->put_Cancel(TRUE);return;}
   DWORD flags=0;dialog->GetOptions(&flags);dialog->SetOptions(flags|FOS_FORCEFILESYSTEM|FOS_OVERWRITEPROMPT);dialog->SetFileName(name.c_str());
   if(FAILED(dialog->Show(window))){pending->put_Cancel(TRUE);return;}Ptr<IShellItem> item;if(FAILED(dialog->GetResult(item.put()))){pending->put_Cancel(TRUE);return;}
   LPWSTR path=nullptr;if(FAILED(item->GetDisplayName(SIGDN_FILESYSPATH,&path))){pending->put_Cancel(TRUE);return;}auto chosen=takeString(path);pending->put_ResultFilePath(chosen.c_str());return; });return S_OK;});
  downloads->add_DownloadStarting(download.get(),&token);
  auto installed=callback<ICoreWebView2AddScriptToExecuteOnDocumentCreatedCompletedHandler,HRESULT,LPCWSTR>([ready](HRESULT result,LPCWSTR){ready(SUCCEEDED(result));return S_OK;});
  checked(tab->web->AddScriptToExecuteOnDocumentCreated(script,installed.get()),"Native authentication initialization failed");
 }
 void add(std::wstring uri,std::function<void(const std::shared_ptr<Tab>&)> complete={}){
  if(tabs.size()>=16){if(complete)complete(nullptr);post([this]{MessageBoxW(window,L"Close an unused Aide tab before opening more pages.",L"Aide",MB_OK);});return;}
  auto tab=std::make_shared<Tab>();tabs.push_back(tab);TCITEMW item{};item.mask=TCIF_TEXT;item.pszText=tab->title.data();TabCtrl_InsertItem(bar,int(tabs.size()-1),&item);select(tabs.size()-1);
  auto ready=callback<ICoreWebView2CreateCoreWebView2ControllerCompletedHandler,HRESULT,ICoreWebView2Controller*>([this,tab,uri,complete](HRESULT result,auto* controller){
   if(tab->closed||FAILED(result)||!controller){if(complete)complete(nullptr);if(!tab->closed)erase(tab);return S_OK;}
   try{tab->controller=Ptr<ICoreWebView2Controller>(controller);checked(controller->get_CoreWebView2(tab->web.put()),"WebView unavailable");configure(tab,[this,tab,uri,complete](bool ready){if(tab->closed||!ready){if(complete)complete(nullptr);if(!tab->closed)erase(tab);return;}resize();select(selected);if(complete)complete(tab);else if(!uri.empty()&&FAILED(tab->web->Navigate(uri.c_str())))fail(L"Aide page navigation failed.");});}
   catch(...){if(complete)complete(nullptr);erase(tab);fail(L"Aide could not initialize its desktop page. Check the WebView2 Runtime and retry.");}return S_OK;});
  if(FAILED(environment->CreateCoreWebView2Controller(window,ready.get()))){if(complete)complete(nullptr);erase(tab);fail(L"Cannot create an Aide app page. Check WebView2 Runtime.");}
 }
 static LRESULT CALLBACK procedure(HWND hwnd,UINT message,WPARAM w,LPARAM l){
  auto self=reinterpret_cast<Desktop*>(GetWindowLongPtrW(hwnd,GWLP_USERDATA));if(message==WM_NCCREATE){self=static_cast<Desktop*>(reinterpret_cast<CREATESTRUCTW*>(l)->lpCreateParams);self->window=hwnd;SetWindowLongPtrW(hwnd,GWLP_USERDATA,reinterpret_cast<LONG_PTR>(self));}
  if(!self)return DefWindowProcW(hwnd,message,w,l);
  switch(message){
   case WM_PAINT:{PAINTSTRUCT paint{};auto dc=BeginPaint(hwnd,&paint);Chrome::fill(dc,paint.rcPaint,self->chrome.background);EndPaint(hwnd,&paint);return 0;}
   case WM_ERASEBKGND:{RECT area{};GetClientRect(hwnd,&area);Chrome::fill(reinterpret_cast<HDC>(w),area,self->chrome.background);return 1;}
   case WM_DRAWITEM:{auto item=reinterpret_cast<DRAWITEMSTRUCT*>(l);if(item->CtlType==ODT_BUTTON){self->chrome.button(*item);return TRUE;}break;}
   case WM_DPICHANGED:{self->chrome.setDpi(HIWORD(w));self->icons();auto bounds=reinterpret_cast<RECT*>(l);SetWindowPos(hwnd,nullptr,bounds->left,bounds->top,bounds->right-bounds->left,bounds->bottom-bounds->top,SWP_NOZORDER|SWP_NOACTIVATE);self->resize();return 0;}
   case WM_SIZE:self->resize();if(w==SIZE_MINIMIZED){self->pause(self->active());self->hello.cancel(self->ceremony);}return 0;
   case WM_SHOWWINDOW:if(!w){self->pause(self->active());self->hello.cancel(self->ceremony);}break;
   case WM_NOTIFY:if(reinterpret_cast<NMHDR*>(l)->hwndFrom==self->bar&&reinterpret_cast<NMHDR*>(l)->code==TCN_SELCHANGE)self->select(TabCtrl_GetCurSel(self->bar));return 0;
   case WM_COMMAND:if(LOWORD(w)==101&&self->environment)self->add(self->entry);else if(LOWORD(w)==102)self->close(self->active());
    else if(LOWORD(w)==103&&self->active()&&self->active()->web)self->active()->web->Reload();
    else if((LOWORD(w)==104||LOWORD(w)==105)&&!self->tabs.empty())self->select((self->selected+(LOWORD(w)==104?1:self->tabs.size()-1))%self->tabs.size());
    else if(LOWORD(w)==106&&self->active()&&self->active()->web)self->active()->web->ExecuteScript(L"if(!document.getElementById('lock-screen')?.hidden)document.getElementById('lock-password')?.focus();else document.getElementById('brand-mini')?.click();",nullptr);return 0;
   case WM_CLOSE:if(l==1){self->hello.cancel(self->ceremony);DestroyWindow(hwnd);}else if(!self->closingAll){self->closingAll=true;self->hello.cancel(self->ceremony);if(self->tabs.empty())DestroyWindow(hwnd);else self->close(self->tabs.back());}return 0;
   case WM_APP+2:{std::unique_ptr<std::function<void()>> task(reinterpret_cast<std::function<void()>*>(l));(*task)();return 0;}
   case WM_APP+3:if(size_t(w)<self->tabs.size())self->close(self->tabs[size_t(w)]);return 0;
   case WM_APP+1:{std::unique_ptr<Reply> reply(reinterpret_cast<Reply*>(l));if(self->ceremony==reply->operation)self->ceremony.reset();if(!reply->operation->cancelled&&!reply->tab->closed&&reply->tab==self->active()&&reply->generation==reply->tab->generation&&IsWindowVisible(hwnd)&&!IsIconic(hwnd))self->send(reply->tab,reply->id,reply->value);return 0;}
   case WM_DESTROY:self->hello.cancel(self->ceremony);PostQuitMessage(0);return 0;
  }
  return DefWindowProcW(hwnd,message,w,l);
 }
 int run(const fs::path& dll,bool alreadyRegistered=false){
  Handle mutex;if(!alreadyRegistered&&!desktopOwnership.valid()){auto mutexName=L"Local\\"+className;mutex.h=CreateMutexW(nullptr,FALSE,mutexName.c_str());if(!mutex.h)throw std::runtime_error("Desktop ownership unavailable");
   if(GetLastError()==ERROR_ALREADY_EXISTS){auto existing=FindWindowW(className.c_str(),nullptr);if(existing){ShowWindow(existing,SW_RESTORE);SetForegroundWindow(existing);}return 0;}}
  FILETIME created{},exited{},kernel{},user{};if(!GetProcessTimes(GetCurrentProcess(),&created,&exited,&kernel,&user))throw std::runtime_error("Cannot identify desktop process lifetime");
  auto creation=(uint64_t(created.dwHighDateTime)<<32)|created.dwLowDateTime;
  struct OwnerReceipt{fs::path file;uint64_t created;~OwnerReceipt(){if(file.empty())return;try{auto value=json::parse(read(file,4096),nullptr,false);if(value.is_object()&&value.value("pid",0u)==GetCurrentProcessId()&&value.value("created",uint64_t(0))==created)fs::remove(file);}catch(...){}}} ownerReceipt{statusFile,creation};
  if(!alreadyRegistered&&!statusFile.empty())write(statusFile,json{{"version",1},{"pid",GetCurrentProcessId()},{"created",creation}}.dump());
  checked(CoInitializeEx(nullptr,COINIT_APARTMENTTHREADED),"Desktop COM unavailable");SetCurrentProcessExplicitAppUserModelID(className.c_str());INITCOMMONCONTROLSEX controls{sizeof(controls),ICC_TAB_CLASSES};InitCommonControlsEx(&controls);
  WNDCLASSW cls{};cls.lpfnWndProc=procedure;cls.hInstance=GetModuleHandleW(nullptr);cls.lpszClassName=className.c_str();cls.hCursor=LoadCursorW(nullptr,IDC_ARROW);cls.hbrBackground=nullptr;cls.hIcon=LoadIconW(cls.hInstance,MAKEINTRESOURCEW(201));RegisterClassW(&cls);
  RECT work{0,0,1412,932};SystemParametersInfoW(SPI_GETWORKAREA,0,&work,0);int width=std::min(1380,std::max(320,int(work.right-work.left)-32)),height=std::min(900,std::max(240,int(work.bottom-work.top)-32));
  window=CreateWindowExW(0,className.c_str(),L"Aide",WS_OVERLAPPEDWINDOW|WS_CLIPCHILDREN,work.left+(work.right-work.left-width)/2,work.top+(work.bottom-work.top-height)/2,width,height,nullptr,nullptr,cls.hInstance,this);if(!window)throw std::runtime_error("Desktop window unavailable");
  chrome.setDpi(GetDpiForWindow(window));icons();
  bar=CreateWindowExW(0,WC_TABCONTROLW,L"",WS_CHILD|WS_VISIBLE|WS_CLIPSIBLINGS|WS_CLIPCHILDREN|TCS_FIXEDWIDTH|TCS_BUTTONS|TCS_FLATBUTTONS,0,0,1200,chrome.height(),window,nullptr,cls.hInstance,nullptr);
  CreateWindowExW(0,L"BUTTON",L"New tab",WS_CHILD|WS_VISIBLE|BS_OWNERDRAW,1205,3,40,42,window,reinterpret_cast<HMENU>(101),cls.hInstance,nullptr);
  chrome.install(bar);resize();
  ShowWindow(window,SW_SHOW);UpdateWindow(window);
  try{computer.start();}catch(...){MessageBoxW(window,L"Aide computer control is unavailable. Check whether port 17778 is already in use.",L"Aide",MB_OK|MB_ICONWARNING);}
  loader=LoadLibraryExW(dll.c_str(),nullptr,LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|LOAD_LIBRARY_SEARCH_SYSTEM32);if(!loader)throw std::runtime_error("Verified WebView2 loader unavailable");
  using Create=HRESULT(STDAPICALLTYPE*)(PCWSTR,PCWSTR,ICoreWebView2EnvironmentOptions*,ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler*);
  auto create=reinterpret_cast<Create>(GetProcAddress(loader,"CreateCoreWebView2EnvironmentWithOptions"));if(!create)throw std::runtime_error("WebView2 loader API unavailable");
  auto ready=callback<ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler,HRESULT,ICoreWebView2Environment*>([this](HRESULT result,auto* value){if(FAILED(result)||!value){fail(L"Aide requires Microsoft Edge WebView2 Runtime. Install the official Runtime, then start Aide again.");return S_OK;}environment=Ptr<ICoreWebView2Environment>(value);add(entry);return S_OK;});
  checked(create(nullptr,(profile/L"WebView2").c_str(),nullptr,ready.get()),"Install Microsoft Edge WebView2 Runtime, then retry");
  ACCEL keys[]={{FVIRTKEY|FCONTROL,'T',101},{FVIRTKEY|FCONTROL,'W',102}};auto accel=CreateAcceleratorTableW(keys,2);MSG msg{};
  while(GetMessageW(&msg,nullptr,0,0)>0){if(!TranslateAcceleratorW(window,accel,&msg)){TranslateMessage(&msg);DispatchMessageW(&msg);}}
  DestroyAcceleratorTable(accel);computer.stop();for(auto& tab:tabs)if(tab->controller)tab->controller->Close();tabs.clear();environment=Ptr<ICoreWebView2Environment>();CoUninitialize();return 0;
 }
};
}
