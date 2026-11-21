#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <winsock2.h>
#include <windows.h>
#include <propidl.h>
#include <objidl.h>
#include <gdiplus.h>
#include <bcrypt.h>
#include <wincrypt.h>
#include <ws2tcpip.h>
#include <thread>
#include <atomic>
#include <iostream>
#include <string>
#include "third_party/json.hpp"
using json=nlohmann::json;
static std::wstring wide(const std::string& value){if(value.empty())return {};int n=MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,value.data(),int(value.size()),nullptr,0);if(!n)throw std::runtime_error("Invalid UTF-8");std::wstring out(size_t(n),0);MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,value.data(),int(value.size()),out.data(),n);return out;}
static std::string utf8(const std::wstring& value){if(value.empty())return {};int n=WideCharToMultiByte(CP_UTF8,WC_ERR_INVALID_CHARS,value.data(),int(value.size()),nullptr,0,nullptr,nullptr);if(!n)throw std::runtime_error("Invalid UTF-16");std::string out(size_t(n),0);WideCharToMultiByte(CP_UTF8,WC_ERR_INVALID_CHARS,value.data(),int(value.size()),out.data(),n,nullptr,nullptr);return out;}
#define AIDE_COMPUTER_BRIDGE_TEST
#include "computer_bridge.hpp"

static HWND windowHandle=nullptr;
static std::atomic<bool> ready=false;
static DWORD uiThreadId=0;
static LRESULT CALLBACK windowProc(HWND window,UINT message,WPARAM w,LPARAM l){if(message==WM_DESTROY){PostQuitMessage(0);return 0;}return DefWindowProcW(window,message,w,l);}
static void uiThread(){
 uiThreadId=GetCurrentThreadId();
 WNDCLASSW cls{};cls.lpfnWndProc=windowProc;cls.hInstance=GetModuleHandleW(nullptr);cls.lpszClassName=L"AideUIASmokeWindow";RegisterClassW(&cls);
 windowHandle=CreateWindowExW(0,cls.lpszClassName,L"Aide UIA smoke",WS_OVERLAPPEDWINDOW|WS_VISIBLE,80,80,520,320,nullptr,nullptr,cls.hInstance,nullptr);
 if(!windowHandle){ready=true;return;}
 CreateWindowExW(0,L"EDIT",L"visible-value",WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_AUTOHSCROLL,20,30,300,28,windowHandle,nullptr,cls.hInstance,nullptr);
 CreateWindowExW(0,L"EDIT",L"must-not-leak",WS_CHILD|WS_VISIBLE|WS_TABSTOP|ES_PASSWORD,20,75,300,28,windowHandle,nullptr,cls.hInstance,nullptr);
 CreateWindowExW(0,L"BUTTON",L"Confirm",WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON,20,120,120,32,windowHandle,nullptr,cls.hInstance,nullptr);
 SetForegroundWindow(windowHandle);ready=true;
 MSG message;while(GetMessageW(&message,nullptr,0,0)>0){TranslateMessage(&message);DispatchMessageW(&message);}
}
int wmain(){
 std::thread ui(uiThread);
 for(int i=0;i<100&&!ready.load();++i)Sleep(25);
 if(!ready.load()){PostThreadMessageW(uiThreadId,WM_QUIT,0,0);ui.join();std::cerr<<"UI window startup timed out\n";return 2;}
 if(!windowHandle){ui.join();std::cerr<<"UI window creation failed\n";return 2;}
 SetForegroundWindow(windowHandle);Sleep(100);
 try{
  auto result=aide_desktop::ComputerBridge::inspectWindowForTest(windowHandle,GetCurrentProcessId(),L"uia-smoke.exe",L"Aide UIA smoke");
  if(result.value("ok",false)!=true||result.value("truncated",true)){std::cerr<<"Unexpected inspect response\n";PostMessageW(windowHandle,WM_CLOSE,0,0);ui.join();return 3;}
  bool foundEdit=false,foundButton=false,leakedSecret=false;
  for(const auto& c:result["controls"]){auto role=c.value("role","");auto label=c.value("label","");auto value=c.value("value","");foundEdit|=role=="AXTextField"&&value=="visible-value";foundButton|=role=="AXButton"&&label=="Confirm";leakedSecret|=value=="must-not-leak"||label=="must-not-leak";}
  PostMessageW(windowHandle,WM_CLOSE,0,0);ui.join();
  if(!foundEdit||!foundButton||leakedSecret){std::cerr<<"UIA contract failure edit="<<foundEdit<<" button="<<foundButton<<" passwordLeak="<<leakedSecret<<"\n";return 4;}
  std::cout<<"PASS Windows UIA returns editable text and button metadata without password values\n";return 0;
 }catch(const std::exception& e){PostMessageW(windowHandle,WM_CLOSE,0,0);ui.join();std::cerr<<"UIA inspect failed: "<<e.what()<<"\n";return 5;}
}
