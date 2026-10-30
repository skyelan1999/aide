#pragma once
// Host-side Buildx authentication uses the CLI process environment. These are
// deliberately not BuildKit-container env options or Dockerfile build args.
#include <algorithm>
#include <map>
#include <stdexcept>
#include <string>
#include <vector>
namespace aide_build_proxy {
using Environment=std::map<std::string,std::string>;
struct System {bool enabled=false;std::string server,bypass,pac;};
struct Plan {Environment additions;std::string source="direct";bool bypassWarning=false;};
inline const char* guidance(){return "Set HTTP_PROXY and HTTPS_PROXY to your HTTP proxy in the same CMD, then call build.bat. PAC/WPAD is not evaluated by the build client; use an explicit proxy endpoint. Existing explicit proxy values are preserved.";}
inline std::string trim(std::string s){auto first=s.find_first_not_of(" \t");return first==s.npos?"":s.substr(first,s.find_last_not_of(" \t")-first+1);}
inline std::string lower(std::string s){for(char& c:s)if(c>='A'&&c<='Z')c+=char('a'-'A');return s;}
inline bool has(const Environment& e,const std::string& key){auto i=e.find(key);return i!=e.end()&&!i->second.empty();}
inline bool explicitProxy(const Environment& e){return has(e,"HTTP_PROXY")||has(e,"HTTPS_PROXY")||has(e,"ALL_PROXY");}
inline std::vector<std::string> split(std::string s){std::vector<std::string> out;size_t offset=0;while(offset<s.size()){auto end=s.find(';',offset);auto part=trim(s.substr(offset,end==s.npos?end:end-offset));if(!part.empty())out.push_back(part);if(end==s.npos)break;offset=end+1;}return out;}
inline bool allowed(const std::string& value,const std::string& punctuation){return value.size()<=2048&&std::all_of(value.begin(),value.end(),[&](unsigned char c){return (c>='a'&&c<='z')||(c>='A'&&c<='Z')||(c>='0'&&c<='9')||punctuation.find(char(c))!=std::string::npos;});}
inline std::runtime_error unsupported(){return std::runtime_error(std::string("Windows manual proxy format is unsupported. ")+guidance());}
inline std::string endpoint(std::string s){
 if(!allowed(s,"-._:[]/"))throw unsupported();
 auto lowered=lower(s);size_t prefix=0;if(lowered.rfind("http://",0)==0)prefix=7;else if(lowered.rfind("https://",0)==0)prefix=8;
 auto host=s.substr(prefix);if(!host.empty()&&host.back()=='/')host.pop_back();
 if(host.empty()||!allowed(host,"-._:[]"))throw unsupported();
 return prefix?s:"http://"+s;
}
inline Plan resolve(const Environment& existing,const System& system){
 Plan result;if(explicitProxy(existing)){result.source="explicit";return result;}
 if(!system.enabled){if(!system.pac.empty())throw std::runtime_error(std::string("Windows PAC proxy detected. ")+guidance());return result;}
 if(system.server.empty()||!allowed(system.server,"-._:[]/;= "))throw unsupported();
 for(auto part:split(system.server)){
  auto equal=part.find('=');if(equal==part.npos){if(result.additions.size())throw unsupported();auto value=endpoint(part);result.additions["HTTP_PROXY"]=value;result.additions["HTTPS_PROXY"]=value;continue;}
  auto protocol=lower(trim(part.substr(0,equal))),value=trim(part.substr(equal+1));
  if(protocol=="http"||protocol=="https"){auto key=protocol=="http"?"HTTP_PROXY":"HTTPS_PROXY";if(result.additions.count(key))throw unsupported();result.additions[key]=endpoint(value);}
  // A Windows SOCKS/FTP endpoint cannot be inferred to be an HTTP proxy.
 }
 if(result.additions.empty())throw unsupported();result.source="system";
 if(!has(existing,"NO_PROXY")&&!system.bypass.empty()){
  std::string bypass;
  for(auto part:split(system.bypass)){
   if(lower(part)=="<local>"){part="localhost,127.0.0.1,[::1]";result.bypassWarning=true;}
   // Go NO_PROXY cannot represent Windows short-name bypass or arbitrary
   // wildcard patterns. Import only equivalent rules and explain the rest.
   else if(!allowed(part,"-._:[]/*")||(part.find('*')!=std::string::npos&&part!="*"&&(part.rfind("*.",0)!=0||part.find('*',1)!=std::string::npos))){result.bypassWarning=true;continue;}
   if(!part.empty()){if(!bypass.empty())bypass+=',';bypass+=part;}
  }
  if(!bypass.empty())result.additions["NO_PROXY"]=bypass;
 }
 return result;
}
inline const char* message(const Plan& plan){return plan.source=="system"?"Build client proxy: Windows manual settings (values hidden).":plan.source=="explicit"?"Build client proxy: explicit environment (values hidden).":"Build client proxy: no explicit or manual proxy; Docker Desktop networking is unchanged.";}
inline const char* bypassMessage(){return "Windows bypass syntax is not fully equivalent to NO_PROXY; loopback rules were imported, but set NO_PROXY explicitly for short hostnames or unsupported wildcard rules.";}
inline const char* failureMessage(){return "Build network/authentication troubleshooting: check that the configured proxy is running and reachable, and that HTTPS_PROXY/NO_PROXY permit auth.docker.io. For BuildKit or Dockerfile RUN networking configure Docker Desktop's container proxy separately; a host 127.0.0.1 endpoint must not be copied unchanged into a Linux container. No system proxy settings were changed.";}
}
