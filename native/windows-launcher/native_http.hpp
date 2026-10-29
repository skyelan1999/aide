#pragma once
// Windows system HTTP stack. Authentication never enters a child command line.
#include <winhttp.h>
#include <array>
#include <functional>
namespace aide_http {
struct Internet {
 HINTERNET value=nullptr;
 explicit Internet(HINTERNET h):value(h){if(!h)throw std::runtime_error("Native HTTP initialization failed");}
 ~Internet(){WinHttpCloseHandle(value);}
 Internet(const Internet&)=delete;Internet& operator=(const Internet&)=delete;
};
struct Response {DWORD status=0;std::string body,location;uint64_t bytes=0;};
inline Response request(const std::string& url,const std::string& method,const std::string& token,const std::string& body,
 const std::function<void(const char*,size_t)>& sink={},uint64_t budget=65536,bool privateLoopback=false){
 auto target=wide(url);URL_COMPONENTS parts{};parts.dwStructSize=sizeof(parts);parts.dwHostNameLength=parts.dwUrlPathLength=parts.dwExtraInfoLength=parts.dwUserNameLength=parts.dwPasswordLength=DWORD(-1);
 if(target.size()>32768||!WinHttpCrackUrl(target.c_str(),DWORD(target.size()),0,&parts)||parts.nScheme!=INTERNET_SCHEME_HTTPS||parts.dwUserNameLength||parts.dwPasswordLength)throw std::runtime_error("Invalid native HTTPS endpoint");
 std::wstring host(parts.lpszHostName,parts.dwHostNameLength),path(parts.lpszUrlPath,parts.dwUrlPathLength);if(parts.dwExtraInfoLength)path.append(parts.lpszExtraInfo,parts.dwExtraInfoLength);
 if(privateLoopback&&(host!=L"127.0.0.1"||!parts.nPort||(path!=L"/api/updates/agent"&&path.rfind(L"/api/updates/agent/",0)!=0&&path!=L"/api/updates/slots")))throw std::runtime_error("Private update endpoint must be the fixed loopback API");
 if(!token.empty()&&(!privateLoopback||token.size()>4096||std::any_of(token.begin(),token.end(),[](unsigned char c){return c<32||c==127;})))throw std::runtime_error("Invalid private API authentication");
 Internet session(WinHttpOpen(L"Aide native host/1",WINHTTP_ACCESS_TYPE_NO_PROXY,WINHTTP_NO_PROXY_NAME,WINHTTP_NO_PROXY_BYPASS,0));
 if(!WinHttpSetTimeouts(session.value,3000,3000,10000,10000))throw std::runtime_error("Cannot bound native HTTP timeouts");
 Internet connection(WinHttpConnect(session.value,host.c_str(),parts.nPort,0));
 Internet query(WinHttpOpenRequest(connection.value,wide(method).c_str(),path.c_str(),nullptr,WINHTTP_NO_REFERER,WINHTTP_DEFAULT_ACCEPT_TYPES,WINHTTP_FLAG_SECURE));
 DWORD disable=WINHTTP_DISABLE_REDIRECTS|WINHTTP_DISABLE_COOKIES|WINHTTP_DISABLE_AUTHENTICATION;
 if(!WinHttpSetOption(query.value,WINHTTP_OPTION_DISABLE_FEATURE,&disable,sizeof(disable)))throw std::runtime_error("Cannot restrict native HTTP request");
 if(privateLoopback){DWORD flags=SECURITY_FLAG_IGNORE_UNKNOWN_CA|SECURITY_FLAG_IGNORE_CERT_CN_INVALID|SECURITY_FLAG_IGNORE_CERT_DATE_INVALID;
  if(!WinHttpSetOption(query.value,WINHTTP_OPTION_SECURITY_FLAGS,&flags,sizeof(flags)))throw std::runtime_error("Cannot configure local development certificate policy");}
 std::wstring headers=L"Content-Type: application/json\r\n";if(!token.empty())headers+=L"Authorization: Bearer "+wide(token)+L"\r\n";
 if(body.size()>65536||!WinHttpSendRequest(query.value,headers.c_str(),DWORD(headers.size()),body.empty()?WINHTTP_NO_REQUEST_DATA:const_cast<char*>(body.data()),DWORD(body.size()),DWORD(body.size()),0)||!WinHttpReceiveResponse(query.value,nullptr))throw std::runtime_error("Native HTTPS request failed");
 Response result;DWORD size=sizeof(result.status);
 if(!WinHttpQueryHeaders(query.value,WINHTTP_QUERY_STATUS_CODE|WINHTTP_QUERY_FLAG_NUMBER,WINHTTP_HEADER_NAME_BY_INDEX,&result.status,&size,WINHTTP_NO_HEADER_INDEX))throw std::runtime_error("Cannot read native HTTP status");
 if(result.status>=300&&result.status<400){DWORD bytes=0;WinHttpQueryHeaders(query.value,WINHTTP_QUERY_LOCATION,WINHTTP_HEADER_NAME_BY_INDEX,nullptr,&bytes,WINHTTP_NO_HEADER_INDEX);
  if(bytes&&bytes<=65536){std::wstring location(bytes/sizeof(wchar_t),0);if(WinHttpQueryHeaders(query.value,WINHTTP_QUERY_LOCATION,WINHTTP_HEADER_NAME_BY_INDEX,location.data(),&bytes,WINHTTP_NO_HEADER_INDEX)){location.resize(wcslen(location.c_str()));result.location=utf8(location);}}return result;}
 if(result.status<200||result.status>=300)return result;
 std::array<char,65536> buffer{};auto started=std::chrono::steady_clock::now();
 while(true){DWORD received=0;if(!WinHttpReadData(query.value,buffer.data(),DWORD(buffer.size()),&received))throw std::runtime_error("Native HTTP response interrupted");if(!received)break;
  if(nativeInterrupted())throw std::runtime_error("Native HTTP transfer stopped by operator");
  if(received>budget-result.bytes)throw std::runtime_error("Native HTTP response exceeds allowed size");result.bytes+=received;
  if(sink)sink(buffer.data(),received);else result.body.append(buffer.data(),received);
  auto seconds=std::chrono::duration_cast<std::chrono::seconds>(std::chrono::steady_clock::now()-started).count();if(seconds>(sink?3600:15))throw std::runtime_error("Native HTTP response timed out");
 }
 return result;
}
}
