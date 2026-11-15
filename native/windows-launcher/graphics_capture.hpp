#pragma once

#include <windows.h>
#include <d3d11.h>
#include <dxgi1_2.h>
#include <chrono>
#include <cstdint>
#include <cstring>
#include <stdexcept>
#include <thread>
#include <inspectable.h>
#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Graphics.Capture.h>
#include <winrt/Windows.Graphics.DirectX.h>
#include <winrt/Windows.Graphics.DirectX.Direct3D11.h>
#include "winrt_imports.hpp"
#include "capture_graphics_interop.hpp"

namespace aide_desktop {
class GraphicsCapture {
 struct Apartment {
  Apartment(){winrt::init_apartment(winrt::apartment_type::multi_threaded);}
  ~Apartment(){winrt::uninit_apartment();}
 };
public:
 static bool supported(){
  try{Apartment apartment;return winrt::Windows::Graphics::Capture::GraphicsCaptureSession::IsSupported();}
  catch(...){return false;}
 }
 static HBITMAP capture(HWND target){
  Apartment apartment;
  using namespace winrt::Windows::Graphics::Capture;
  using namespace winrt::Windows::Graphics::DirectX;
  using namespace winrt::Windows::Graphics::DirectX::Direct3D11;
  auto interop=winrt::get_activation_factory<GraphicsCaptureItem,IGraphicsCaptureItemInterop>();
  GraphicsCaptureItem item{nullptr};
  winrt::check_hresult(interop->CreateForWindow(target,winrt::guid_of<GraphicsCaptureItem>(),winrt::put_abi(item)));
  const auto initialSize=item.Size();
  auto valid=[](int width,int height){return width>0&&height>0&&width<=4096&&height<=4096&&int64_t(width)*height<=4000000;};
  if(!valid(initialSize.Width,initialSize.Height))throw std::runtime_error("Foreground window exceeds screenshot limits");

  winrt::com_ptr<ID3D11Device> device;
  winrt::com_ptr<ID3D11DeviceContext> context;
  D3D_FEATURE_LEVEL level{};
  auto result=D3D11CreateDevice(nullptr,D3D_DRIVER_TYPE_HARDWARE,nullptr,D3D11_CREATE_DEVICE_BGRA_SUPPORT,
    nullptr,0,D3D11_SDK_VERSION,device.put(),&level,context.put());
  if(FAILED(result))result=D3D11CreateDevice(nullptr,D3D_DRIVER_TYPE_WARP,nullptr,D3D11_CREATE_DEVICE_BGRA_SUPPORT,
    nullptr,0,D3D11_SDK_VERSION,device.put(),&level,context.put());
  if(FAILED(result))throw winrt::hresult_error(result);
  winrt::com_ptr<IDXGIDevice> dxgi;
  device.as(dxgi);
  winrt::com_ptr<IInspectable> projectedDevice;
  winrt::check_hresult(CreateDirect3D11DeviceFromDXGIDevice(dxgi.get(),projectedDevice.put()));
  auto captureDevice=projectedDevice.as<IDirect3DDevice>();
  auto pool=Direct3D11CaptureFramePool::CreateFreeThreaded(captureDevice,DirectXPixelFormat::B8G8R8A8UIntNormalized,2,initialSize);
  auto session=pool.CreateCaptureSession(item);
  session.IsCursorCaptureEnabled(false);
  session.StartCapture();

  Direct3D11CaptureFrame frame{nullptr};
  const auto started=std::chrono::steady_clock::now();
  const auto deadline=started+std::chrono::milliseconds(1800);
  while(std::chrono::steady_clock::now()<deadline){
   auto next=pool.TryGetNextFrame();
   if(next)frame=std::move(next);
   if(frame&&std::chrono::steady_clock::now()-started>=std::chrono::milliseconds(120))break;
   std::this_thread::sleep_for(std::chrono::milliseconds(20));
  }
  if(!frame){session.Close();pool.Close();throw std::runtime_error("Windows did not deliver a foreground-window capture frame");}
  const auto size=frame.ContentSize();
  if(!valid(size.Width,size.Height)){session.Close();pool.Close();throw std::runtime_error("Captured window exceeds screenshot limits");}
  auto surface=frame.Surface().as<IDirect3DDxgiInterfaceAccess>();
  winrt::com_ptr<ID3D11Texture2D> source;
  winrt::check_hresult(surface->GetInterface(__uuidof(ID3D11Texture2D),source.put_void()));
  D3D11_TEXTURE2D_DESC desc{};source->GetDesc(&desc);
  if(desc.Width!=UINT(size.Width)||desc.Height!=UINT(size.Height)||desc.Format!=DXGI_FORMAT_B8G8R8A8_UNORM||desc.SampleDesc.Count!=1){
   session.Close();pool.Close();throw std::runtime_error("Windows returned an unsupported capture surface");
  }
  desc.Usage=D3D11_USAGE_STAGING;desc.BindFlags=0;desc.MiscFlags=0;desc.CPUAccessFlags=D3D11_CPU_ACCESS_READ;
  winrt::com_ptr<ID3D11Texture2D> staging;
  winrt::check_hresult(device->CreateTexture2D(&desc,nullptr,staging.put()));
  context->CopyResource(staging.get(),source.get());context->Flush();
  D3D11_MAPPED_SUBRESOURCE mapped{};
  winrt::check_hresult(context->Map(staging.get(),0,D3D11_MAP_READ,0,&mapped));
  BITMAPINFO info{};info.bmiHeader.biSize=sizeof(BITMAPINFOHEADER);info.bmiHeader.biWidth=size.Width;info.bmiHeader.biHeight=-size.Height;
  info.bmiHeader.biPlanes=1;info.bmiHeader.biBitCount=32;info.bmiHeader.biCompression=BI_RGB;
  HDC screen=GetDC(nullptr);void* pixels=nullptr;HBITMAP bitmap=CreateDIBSection(screen,&info,DIB_RGB_COLORS,&pixels,nullptr,0);ReleaseDC(nullptr,screen);
  if(!bitmap){context->Unmap(staging.get(),0);session.Close();pool.Close();throw std::runtime_error("Cannot allocate screenshot buffer");}
  auto* destination=static_cast<BYTE*>(pixels);auto* origin=static_cast<const BYTE*>(mapped.pData);const size_t stride=size_t(size.Width)*4;
  for(int y=0;y<size.Height;y++)memcpy(destination+size_t(y)*stride,origin+size_t(y)*mapped.RowPitch,stride);
  context->Unmap(staging.get(),0);session.Close();pool.Close();return bitmap;
 }
};
}
