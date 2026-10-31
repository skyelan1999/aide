#pragma once
#include <winhttp.h>
#include "build_proxy.hpp"
// Included after the native Unicode/environment helpers in main.cpp.
inline aide_build_proxy::Plan configureBuildProxy(){
 aide_build_proxy::Environment existing;
 for(const auto* name:{L"HTTP_PROXY",L"HTTPS_PROXY",L"ALL_PROXY",L"NO_PROXY"})existing[utf8(name)]=utf8(env(name));
 aide_build_proxy::System system;
 if(!aide_build_proxy::explicitProxy(existing)){
  WINHTTP_CURRENT_USER_IE_PROXY_CONFIG config{};
  if(!WinHttpGetIEProxyConfigForCurrentUser(&config)){
   if(GetLastError()!=ERROR_FILE_NOT_FOUND)throw std::runtime_error(std::string("Cannot read Windows build proxy settings. ")+aide_build_proxy::guidance());
  }else{
   struct Cleanup{WINHTTP_CURRENT_USER_IE_PROXY_CONFIG& c;~Cleanup(){GlobalFree(c.lpszProxy);GlobalFree(c.lpszProxyBypass);GlobalFree(c.lpszAutoConfigUrl);}}cleanup{config};
   if(config.lpszProxy){system.enabled=true;system.server=utf8(config.lpszProxy);}
   if(config.lpszProxyBypass)system.bypass=utf8(config.lpszProxyBypass);
   if(config.lpszAutoConfigUrl)system.pac=utf8(config.lpszAutoConfigUrl);
  }
 }
 auto plan=aide_build_proxy::resolve(existing,system);
 for(const auto& value:plan.additions)setenvw(wide(value.first).c_str(),wide(value.second));
 return plan;
}
