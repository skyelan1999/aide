#pragma once

#include <unknwn.h>

struct __declspec(uuid("3628E81B-3CAC-4C60-B7F4-23CE0E0C3356")) IGraphicsCaptureItemInterop : IUnknown {
 virtual HRESULT STDMETHODCALLTYPE CreateForWindow(HWND window, REFIID iid, void** result) = 0;
 virtual HRESULT STDMETHODCALLTYPE CreateForMonitor(HMONITOR monitor, REFIID iid, void** result) = 0;
};

struct __declspec(uuid("A9B3D012-3DF2-4EE3-B8D1-8695F457D3C1")) IDirect3DDxgiInterfaceAccess : IUnknown {
 virtual HRESULT STDMETHODCALLTYPE GetInterface(REFIID iid, void** result) = 0;
};

namespace winrt::impl {
template<> inline constexpr guid guid_v<::IGraphicsCaptureItemInterop>{0x3628e81b,0x3cac,0x4c60,{0xb7,0xf4,0x23,0xce,0x0e,0x0c,0x33,0x56}};
template<> inline constexpr guid guid_v<::IDirect3DDxgiInterfaceAccess>{0xa9b3d012,0x3df2,0x4ee3,{0xb8,0xd1,0x86,0x95,0xf4,0x57,0xd3,0xc1}};
}

extern "C" HRESULT WINAPI CreateDirect3D11DeviceFromDXGIDevice(IDXGIDevice* dxgiDevice, IInspectable** graphicsDevice);
