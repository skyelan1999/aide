#pragma once
#include <string>
#include <algorithm>
namespace aide_desktop {
inline bool controls(const std::wstring& u) {
 return std::any_of(u.begin(),u.end(),[](wchar_t c){return c<32||c==127||c==L'\\';});
}
inline bool local(const std::wstring& u,const std::wstring& origin) {
 if(controls(u))return false;
 if(u.rfind(L"blob:",0)==0)return u.rfind(L"blob:blob:",0)!=0&&local(u.substr(5),origin);
 return u.rfind(origin+L"/",0)==0;
}
inline bool authRoot(const std::wstring& u,const std::wstring& origin) {return u==origin+L"/";}
inline bool external(const std::wstring& u) {
 if(controls(u))return false;
 auto offset=u.rfind(L"https://",0)==0?8:u.rfind(L"http://",0)==0?7:0;
 if(!offset)return u.rfind(L"mailto:",0)==0;
 auto end=u.find_first_of(L"/?#",offset);auto authority=u.substr(offset,end-offset);
 return !authority.empty()&&authority.find(L'@')==authority.npos;
}
}
