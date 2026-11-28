#pragma once
// Pure planning shared with POSIX fixtures: never execute output or change tags.
#include <map>
#include <regex>
#include <sstream>
#include <string>
namespace aide_build_sources {
using Values=std::map<std::string,std::string>;
inline bool uncredentialedPipIndex(const std::string& index){
 // The public builder arg must not carry userinfo, queries or fragments.
 if(index.find_first_of("?#\\")!=index.npos)return false;
 for(unsigned char c:index)if(c<=32||c==127)return false;
 return std::regex_match(index,std::regex("https?://[^/@]+(/.*)?"));
}
inline Values fallback(const std::string& output,const std::string& dockerfile,const Values& arguments){
 auto disabled=arguments.find("AIDE_CN_SOURCE_FALLBACK");if(disabled!=arguments.end()&&disabled->second=="0")return {};
 std::string errors;std::istringstream lines(output);std::string line;
 while(std::getline(lines,line))if(line.find("ERROR")!=line.npos||line.find("error:")!=line.npos)errors+=line+"\n";
 if(!std::regex_search(errors,std::regex("failed to (resolve source metadata|authorize|fetch (anonymous |oauth )?token|do request)|pull access denied|unexpected status from (HEAD|GET)")))return {};
 Values result;
 for(auto key:{"PYTHON_BASE","NODE_BASE","GO_BASE"}){
  std::string ref;auto supplied=arguments.find(key);
  if(supplied!=arguments.end())ref=supplied->second;
  else {std::smatch match;if(std::regex_search(dockerfile,match,std::regex(std::string("(^|\\n)ARG ")+key+"=([^\\r\\n ]+)")))ref=match[2];}
  if(ref.empty())continue;
  std::string canonical=ref;
  for(auto prefix:{"docker.io/library/","docker.io/","library/"})if(canonical.rfind(prefix,0)==0){canonical=canonical.substr(std::string(prefix).size());break;}
  // Only public official images. Local/custom/private registries stay untouched.
  if(!std::regex_match(canonical,std::regex("(python|node|golang)(:[A-Za-z0-9_.-]+)?(@sha256:[a-f0-9]{64})?")))continue;
  if(errors.find(ref)==errors.npos&&errors.find(canonical)==errors.npos)continue;
  result[key]="docker.m.daocloud.io/library/"+canonical;
 }
 return result;
}
}
