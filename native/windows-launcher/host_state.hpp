#pragma once
// Shared host update protocol/transaction core. OS actions are supplied by Host;
// the same production state machine is executable in portable fault tests.
#include "third_party/json.hpp"
#include <regex>
#include <algorithm>
#include <sstream>
#include <string>
#include <stdexcept>
namespace aide_host {
using Json=nlohmann::json;
struct Unsafe : std::runtime_error {using std::runtime_error::runtime_error;};
struct Command {
 std::string operation,target,package,tag,image,platform,hash,reference,version;
 Json json()const{return {operation,target,package,tag,image,platform,hash,reference,version};}
 static Command parse(const Json& fields){
  if(!fields.is_array()||fields.size()!=9)throw std::runtime_error("Invalid update command field count");
  for(const auto& f:fields)if(!f.is_string()||f.get_ref<const std::string&>().size()>256)throw std::runtime_error("Invalid update command field");
  Command c{fields[0],fields[1],fields[2],fields[3],fields[4],fields[5],fields[6],fields[7],fields[8]};
  if(c.package=="-")c.package.clear();if(c.hash=="-")c.hash.clear();
  auto match=[](const std::string& s,const char* pattern){return std::regex_match(s,std::regex(pattern));};
  if(!match(c.operation,"[a-f0-9]{32}")||(c.target!="A"&&c.target!="B")||(!c.package.empty()&&!match(c.package,"[a-f0-9]{32}"))||
     !match(c.tag,"v[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+-RC[0-9]+")||!match(c.image,"sha256:[a-f0-9]{64}")||
     (c.platform!="linux/amd64"&&c.platform!="linux/arm64")||c.reference!=(c.target=="A"?"aide:slot-a":"aide:slot-b")||
     (!c.package.empty()&&!match(c.hash,"[a-f0-9]{64}"))||(!c.hash.empty()&&!match(c.hash,"[a-f0-9]{64}"))||
     c.version.empty()||std::any_of(c.version.begin(),c.version.end(),[](unsigned char x){return x<32||x==127;}))
   throw std::runtime_error("Invalid update command identity");
  return c;
 }
 static Command tsv(const std::string& line){Json f=Json::array();size_t begin=0;while(true){auto end=line.find('\t',begin);f.push_back(line.substr(begin,end==line.npos?line.npos:end-begin));if(end==line.npos)break;begin=end+1;}return parse(f);}
};
inline std::string diagnostic(std::string text,const std::string& token={},size_t limit=220){
 if(!token.empty())for(size_t i=0;(i=text.find(token,i))!=text.npos;i+=10)text.replace(i,token.size(),"[REDACTED]");
 text=std::regex_replace(text,std::regex("([#?&](?:access[-_]?token|token|password|secret|key)=)[^\\s\"'<>]+",std::regex::icase),"$1[REDACTED]");
 text=std::regex_replace(text,std::regex("(Bearer\\s+)[^\\s]+",std::regex::icase),"$1[REDACTED]");
 text=std::regex_replace(text,std::regex("((?:access[-_ ]?token|api[-_]?key|password|secret)\\s*[:=]\\s*)(?:\"[^\"]*\"|'[^']*'|[^\\s]+)",std::regex::icase),"$1[REDACTED]");
 for(auto& c:text)if(static_cast<unsigned char>(c)<32||c==127)c=' ';
 if(text.size()>limit)text.resize(limit);return text;
}
// Compose config serializes literal dollars doubled for safe round-trip.
inline std::string composeLiteral(const std::string& value){std::string result;for(size_t i=0;i<value.size();i++){result+=value[i];if(value[i]=='$'&&i+1<value.size()&&value[i+1]=='$')i++;}return result;}
inline std::string checksum(const std::string& text,const std::string& filename){
 std::istringstream input(text);std::string hash,name,found;while(input>>hash>>name){if(!name.empty()&&name[0]=='*')name.erase(0,1);if(name==filename){if(!found.empty()||!std::regex_match(hash,std::regex("[a-f0-9]{64}")))throw std::runtime_error("Invalid duplicate Release checksum");found=hash;}}
 if(found.empty())throw std::runtime_error("No unique SHA256 for the requested Release archive");return found;
}
enum class Ack {retry,accepted,obsolete};
template<class Host> class Supervisor {
 Host& host;
 void result(Json& j,bool success,const std::string& message){j["phase"]="result";j["result"]={{"operationId",j["command"][0]},{"success",success},{"message",host.redact(message)}};host.store(j);}
 void recover(Json& j,const Command& c,const std::string& failure){
  try{host.rollback(j.at("oldMarker").template get<std::string>());result(j,false,"Activation failed: "+failure+"; Recovery: previous image and health verified");}
  catch(const Unsafe&){j["phase"]="blocked";j["reason"]="Recovery child termination unconfirmed; manual inspection required";host.store(j);}
  catch(const std::exception& e){result(j,false,"Activation failed: "+failure+"; Recovery failed: "+host.redact(e.what()));}
 }
public:
 explicit Supervisor(Host& h):host(h){}
 void tick(){
  auto j=host.load();bool fresh=j.is_null();
  if(fresh){auto line=host.command();if(line.empty())return;auto c=Command::tsv(line);j={{"version",1},{"command",c.json()},{"oldMarker",host.marker()},{"phase","preparing"}};host.store(j);}
  if(!j.is_object()||j.value("version",0)!=1||!j.contains("command")||!j.contains("oldMarker")||!j["oldMarker"].is_string()||j["oldMarker"].template get_ref<const std::string&>().size()>4096)throw std::runtime_error("Invalid persistent native update transaction");
  auto c=Command::parse(j["command"]);auto phase=j.value("phase","");
  if(j.contains("canonical")&&(!j["canonical"].is_string()||!std::regex_match(j["canonical"].template get<std::string>(),std::regex("sha256:[a-f0-9]{64}"))))throw std::runtime_error("Invalid durable canonical image identity");
  if(phase=="blocked")return;
  if(fresh){
   try{
    auto canonical=host.prepare(c);if(!std::regex_match(canonical,std::regex("sha256:[a-f0-9]{64}")))throw std::runtime_error("Invalid canonical target image");
    j["canonical"]=canonical;j["phase"]="activating";host.store(j);
    host.activate(c,canonical);host.verify(c,canonical);result(j,true,"Target image identity and health verified");
   }catch(const Unsafe&){j["phase"]="blocked";j["reason"]="Native child termination unconfirmed; recovery paused";host.store(j);return;}
   catch(const std::exception& e){recover(j,c,host.redact(e.what()));}
  }else if(phase=="preparing"||phase=="activating")recover(j,c,"Previous native agent was interrupted");
  else if(phase!="result"&&phase!="sync")throw std::runtime_error("Invalid native update transaction phase");
  if(j.value("phase","")=="blocked")return;
  if(j.value("phase","")=="result"){
   if(!j.contains("result")||j["result"].value("operationId","")!=c.operation||!j["result"].contains("success")||!j["result"]["success"].is_boolean()||!j["result"].contains("message")||!j["result"]["message"].is_string()||j["result"]["message"].template get_ref<const std::string&>().size()>1024)throw std::runtime_error("Invalid durable update result");
   auto ack=host.submit(j["result"]);if(ack==Ack::retry)return;
   if(ack==Ack::obsolete){host.clear();return;}
   if(!j["result"]["success"].template get<bool>()){host.clear();return;}
   j["phase"]="sync";host.store(j);
  }
  if(j.value("phase","")=="sync"&&host.sync(c,j.at("canonical").template get<std::string>()))host.clear();
 }
};
}
