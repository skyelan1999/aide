#pragma once

#include <windows.h>
#include <winrt/base.h>

namespace aide_desktop::winrt_imports {
template<class Function>
Function resolve(char const* name) noexcept {
 HMODULE module=GetModuleHandleW(L"combase.dll");
 if(!module)module=LoadLibraryW(L"combase.dll");
 return module?reinterpret_cast<Function>(GetProcAddress(module,name)):nullptr;
}
}

extern "C" int32_t __stdcall WINRT_IMPL_RoGetActivationFactory(void* classId,winrt::guid const& iid,void** factory) noexcept {
 using Function=int32_t(WINAPI*)(void*,winrt::guid const&,void**) noexcept;
 auto call=aide_desktop::winrt_imports::resolve<Function>("RoGetActivationFactory");
 return call?call(classId,iid,factory):HRESULT_FROM_WIN32(ERROR_PROC_NOT_FOUND);
}

extern "C" int32_t __stdcall WINRT_IMPL_RoOriginateLanguageException(int32_t error,void* message,void* exception) noexcept {
 using Function=int32_t(WINAPI*)(int32_t,void*,void*) noexcept;
 auto call=aide_desktop::winrt_imports::resolve<Function>("RoOriginateLanguageException");
 return call?call(error,message,exception):HRESULT_FROM_WIN32(ERROR_PROC_NOT_FOUND);
}
