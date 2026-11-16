#include <windows.h>
#include <iostream>
#include "../native/windows-launcher/graphics_capture.hpp"

static LRESULT CALLBACK windowProcedure(HWND hwnd,UINT message,WPARAM w,LPARAM l){
 if(message==WM_PAINT){PAINTSTRUCT paint{};BeginPaint(hwnd,&paint);EndPaint(hwnd,&paint);return 0;}
 return DefWindowProcW(hwnd,message,w,l);
}

int main(){
 const wchar_t* className=L"AideGraphicsCaptureSmoke";WNDCLASSEXW wc{sizeof(wc)};wc.lpfnWndProc=windowProcedure;wc.hInstance=GetModuleHandleW(nullptr);wc.lpszClassName=className;
 if(!RegisterClassExW(&wc))return 1;
 HWND window=CreateWindowExW(WS_EX_TOPMOST,className,L"Aide GPU capture fixture",WS_POPUP,80,80,320,240,nullptr,nullptr,wc.hInstance,nullptr);
 if(!window){UnregisterClassW(className,wc.hInstance);return 2;}ShowWindow(window,SW_SHOW);SetForegroundWindow(window);UpdateWindow(window);
 DXGI_SWAP_CHAIN_DESC swapDesc{};swapDesc.BufferDesc.Width=320;swapDesc.BufferDesc.Height=240;swapDesc.BufferDesc.Format=DXGI_FORMAT_R8G8B8A8_UNORM;swapDesc.SampleDesc.Count=1;swapDesc.BufferUsage=DXGI_USAGE_RENDER_TARGET_OUTPUT;swapDesc.BufferCount=2;swapDesc.OutputWindow=window;swapDesc.Windowed=TRUE;swapDesc.SwapEffect=DXGI_SWAP_EFFECT_DISCARD;
 D3D_FEATURE_LEVEL level{};ID3D11Device* device=nullptr;ID3D11DeviceContext* context=nullptr;IDXGISwapChain* swap=nullptr;
 HRESULT created=D3D11CreateDeviceAndSwapChain(nullptr,D3D_DRIVER_TYPE_HARDWARE,nullptr,D3D11_CREATE_DEVICE_BGRA_SUPPORT,nullptr,0,D3D11_SDK_VERSION,&swapDesc,&swap,&device,&level,&context);
 if(FAILED(created)){created=D3D11CreateDeviceAndSwapChain(nullptr,D3D_DRIVER_TYPE_WARP,nullptr,D3D11_CREATE_DEVICE_BGRA_SUPPORT,nullptr,0,D3D11_SDK_VERSION,&swapDesc,&swap,&device,&level,&context);}
 if(FAILED(created)){DestroyWindow(window);UnregisterClassW(className,wc.hInstance);return 6;}
 ID3D11Texture2D* backBuffer=nullptr;ID3D11RenderTargetView* target=nullptr;
 if(FAILED(swap->GetBuffer(0,__uuidof(ID3D11Texture2D),reinterpret_cast<void**>(&backBuffer)))||FAILED(device->CreateRenderTargetView(backBuffer,nullptr,&target))){if(backBuffer)backBuffer->Release();swap->Release();context->Release();device->Release();DestroyWindow(window);UnregisterClassW(className,wc.hInstance);return 7;}
 const float knownColor[4]{.90f,.10f,.20f,1.0f};context->OMSetRenderTargets(1,&target,nullptr);context->ClearRenderTargetView(target,knownColor);swap->Present(1,0);Sleep(750);target->Release();backBuffer->Release();
 HBITMAP image=nullptr;
 try{image=aide_desktop::GraphicsCapture::capture(window);}catch(const std::exception& error){std::cerr<<"FAIL Windows Graphics Capture: "<<error.what()<<"\n";DestroyWindow(window);UnregisterClassW(className,wc.hInstance);return 3;}
 BITMAP bitmap{};if(!image||GetObjectW(image,sizeof(bitmap),&bitmap)!=sizeof(bitmap)||bitmap.bmWidth<320||bitmap.bmHeight<240){if(image)DeleteObject(image);DestroyWindow(window);UnregisterClassW(className,wc.hInstance);return 4;}
 HDC dc=CreateCompatibleDC(nullptr);auto old=SelectObject(dc,image);
 const auto actual=GetPixel(dc,bitmap.bmWidth/2,bitmap.bmHeight/2);const int expected[3]{230,26,51};
 std::cerr<<"Captured "<<bitmap.bmWidth<<"x"<<bitmap.bmHeight<<" center RGB "<<int(GetRValue(actual))<<","<<int(GetGValue(actual))<<","<<int(GetBValue(actual))<<" expected 230,26,51\n";
 bool valid=actual!=CLR_INVALID&&std::abs(int(GetRValue(actual))-expected[0])<=12&&std::abs(int(GetGValue(actual))-expected[1])<=12&&std::abs(int(GetBValue(actual))-expected[2])<=12;
 std::cerr<<"Pixel check "<<(valid?"PASS":"FAIL")<<" raw=0x"<<std::hex<<actual<<std::dec<<"\n";
 HDC screen=GetDC(nullptr),printDc=CreateCompatibleDC(screen);HBITMAP printBitmap=CreateCompatibleBitmap(screen,320,240);auto oldPrint=SelectObject(printDc,printBitmap);BOOL printOk=PrintWindow(window,printDc,PW_RENDERFULLCONTENT);auto printPixel=GetPixel(printDc,160,120);std::cerr<<"Fixture foreground="<<(GetForegroundWindow()==window)<<" PrintWindow="<<printOk<<" RGB "<<int(GetRValue(printPixel))<<","<<int(GetGValue(printPixel))<<","<<int(GetBValue(printPixel))<<"\n";SelectObject(printDc,oldPrint);DeleteObject(printBitmap);DeleteDC(printDc);ReleaseDC(nullptr,screen);
 SelectObject(dc,old);DeleteDC(dc);DeleteObject(image);swap->Release();context->Release();device->Release();DestroyWindow(window);UnregisterClassW(className,wc.hInstance);
 if(!valid){std::cerr<<"FAIL Windows Graphics Capture GPU-frame pixel validation\n";return 5;}
 std::cout<<"PASS Windows Graphics Capture returned expected GPU-rendered HWND pixels "<<bitmap.bmWidth<<"x"<<bitmap.bmHeight<<"\n";return 0;
}
