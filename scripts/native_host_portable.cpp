#include "../native/windows-launcher/host_state.hpp"
#include <iostream>
#include <vector>
using aide_host::Json;
struct Crash {};
struct Host {
 Json state=nullptr,posted;std::vector<std::string> calls;std::string failure,crashPhase;
 int acknowledgements=0,syncs=0;bool retry=false,obsolete=false,syncRetry=false;
 std::string id="sha256:"+std::string(64,'b');
 Json load(){return state;}
 std::string command(){calls.push_back("command");return std::string(32,'a')+"\tB\t-\tv0.1.17.5-RC12\tsha256:"+std::string(64,'c')+"\tlinux/amd64\t-\taide:slot-b\t0.1.17.5 RC12";}
 std::string marker(){return "aide:slot-a sha256:"+std::string(64,'d')+" linux/amd64 v0.1.17.4-RC11";}
 std::string redact(const std::string& s){return aide_host::diagnostic(s,"owned-secret");}
 void store(const Json& j){state=j;if(j.value("phase","")==crashPhase)throw Crash{};}
 void clear(){calls.push_back("clear");state=nullptr;}
 std::string prepare(const aide_host::Command&){calls.push_back("prepare");if(failure=="prepare")throw std::runtime_error("prepare failed owned-secret");return id;}
 void activate(const aide_host::Command&,const std::string&){calls.push_back("activate");if(failure=="unsafe")throw aide_host::Unsafe("owned child still active");if(failure=="activate")throw std::runtime_error("activation failed");}
 void verify(const aide_host::Command&,const std::string&){calls.push_back("verify");if(failure=="verify"||failure=="rollback"||failure=="rollback-unsafe")throw std::runtime_error("target unhealthy");}
 void rollback(const std::string& marker){calls.push_back("rollback");if(marker!=this->marker())throw std::runtime_error("old marker changed");if(failure=="rollback")throw std::runtime_error("old image unhealthy");if(failure=="rollback-unsafe")throw aide_host::Unsafe("recovery child still active");}
 aide_host::Ack submit(const Json& body){acknowledgements++;posted=body;if(retry)return aide_host::Ack::retry;return obsolete?aide_host::Ack::obsolete:aide_host::Ack::accepted;}
 bool sync(const aide_host::Command&,const std::string& canonical){syncs++;if(canonical!=id)throw std::runtime_error("wrong canonical ID");return !syncRetry;}
 int count(const std::string& c){return int(std::count(calls.begin(),calls.end(),c));}
};
void require(bool value,const char* message){if(!value)throw std::runtime_error(message);}
int main(){try{
 int checks=0;auto pass=[&](const char* name){std::cout<<"PASS "<<name<<'\n';checks++;};
 for(const auto& sample:std::vector<std::pair<std::string,std::string>>{{"C:/plain","C:/plain"},{"C:/cost$$money","C:/cost$money"},{"C:/$${USER}/$$$$literal","C:/${USER}/$$literal"}}){require(aide_host::composeLiteral(sample.first)==sample.second,"Compose literal bind path changed");pass("resolved Compose bind source preserves literal dollars");}
 for(const auto& prefix:{"  "," *"}){auto expected=std::string(64,'e');require(aide_host::checksum(expected+prefix+"image.tar.gz\r\n","image.tar.gz")==expected,"checksum parse failed");pass("tagged thin-package checksum supports text/binary SHA256SUMS");}
 for(const auto& bad:{std::string("bad  image.tar.gz\n"),std::string(64,'e')+"  image.tar.gz\n"+std::string(64,'f')+"  image.tar.gz\n",std::string(64,'e')+"  unrelated.tar.gz\n"}){bool rejected=false;try{aide_host::checksum(bad,"image.tar.gz");}catch(const std::exception&){rejected=true;}require(rejected,"ambiguous/unpinned image checksum accepted");pass("tagged thin-package refuses invalid/duplicate/missing checksum");}
 Host fixture;auto valid=aide_host::Command::tsv(fixture.command()).json();
 for(const auto& sample:std::vector<std::pair<int,std::string>>{{0,"../escape"},{1,"C"},{2,"../../package"},{3,"v0.1;run"},{4,"sha256:bad"},{5,"windows/amd64"},{6,"bad-hash"},{7,"--shell"},{8,"private\nvalue"}}){auto bad=valid;bad[sample.first]=sample.second;bool rejected=false;try{aide_host::Command::parse(bad);}catch(const std::exception&){rejected=true;}require(rejected,"invalid command accepted");pass("reject malformed protocol field");}
 {Host h;aide_host::Supervisor<Host>(h).tick();require(h.state.is_null()&&h.count("activate")==1&&h.posted["success"]==true&&h.syncs==1,"healthy activation failed");pass("verified target, canonical sync, acknowledged durable result");}
 {Host h;h.retry=true;aide_host::Supervisor<Host> s(h);s.tick();require(h.state["phase"]=="result","result not durable");s.tick();require(h.count("activate")==1&&h.acknowledgements==2,"lost result acknowledgement replayed activation");h.retry=false;s.tick();require(h.state.is_null(),"result retry not cleared");pass("lost acknowledgement retries result without activation replay");}
 {Host h;h.obsolete=true;aide_host::Supervisor<Host>(h).tick();require(h.state.is_null()&&h.syncs==0,"obsolete operation overwrote newer slot state");pass("obsolete result does not sync stale state");}
 {Host h;h.syncRetry=true;aide_host::Supervisor<Host> s(h);s.tick();require(h.state["phase"]=="sync","canonical sync lost");h.syncRetry=false;s.tick();require(h.state.is_null()&&h.count("activate")==1&&h.acknowledgements==1&&h.syncs==2,"sync retry repeated result/activation");pass("canonical sync durable across lost acknowledgement");}
 for(const auto& phase:{"preparing","activating"}){Host h;h.crashPhase=phase;aide_host::Supervisor<Host> s(h);try{s.tick();}catch(const Crash&){}require(h.state["phase"]==phase,"interruption not persisted");h.crashPhase.clear();s.tick();require(h.count("rollback")==1&&h.posted["success"]==false&&h.state.is_null(),"interrupted transaction not recovered");pass("interrupted transaction restores old verified slot without target replay");}
 for(const auto& failure:{"prepare","activate","verify"}){Host h;h.failure=failure;aide_host::Supervisor<Host>(h).tick();require(h.count("rollback")==1&&h.posted["success"]==false&&h.posted.dump().find("owned-secret")==std::string::npos,"failure recovery/diagnostic failed");pass("failed stage restores old image and redacts persisted result");}
 {Host h;h.failure="unsafe";aide_host::Supervisor<Host> s(h);s.tick();s.tick();require(h.state["phase"]=="blocked"&&h.count("rollback")==0&&h.acknowledgements==0&&h.count("activate")==1,"unsafe child raced recovery");pass("unconfirmed child tree stops automatic activation and recovery");}
 {Host h;h.failure="rollback-unsafe";aide_host::Supervisor<Host>(h).tick();require(h.state["phase"]=="blocked"&&h.acknowledgements==0,"unconfirmed recovery child was acknowledged");pass("unconfirmed recovery tree keeps blocker durable");}
 {Host h;h.failure="rollback";aide_host::Supervisor<Host>(h).tick();require(h.posted["success"]==false&&h.posted["message"].get<std::string>().find("Recovery failed")!=std::string::npos,"recovery failure hidden");pass("failed rollback is reported separately from target failure");}
 {Host h;h.state={{"version",1},{"command",valid},{"oldMarker",h.marker()},{"phase","unexpected"}};bool rejected=false;try{aide_host::Supervisor<Host>(h).tick();}catch(const std::exception&){rejected=true;}require(rejected&&h.calls.empty(),"corrupt state executed commands");pass("malformed durable state fails closed");}
 for(const auto& field:{"canonical","result-operation","result-success","result-message"}){Host h;h.retry=true;aide_host::Supervisor<Host> agent(h);agent.tick();int activations=h.count("activate"),acks=h.acknowledgements;if(std::string(field)=="canonical")h.state["canonical"]="not-an-image";else if(std::string(field)=="result-operation")h.state["result"]["operationId"]=std::string(32,'f');else if(std::string(field)=="result-success")h.state["result"]["success"]="true";else h.state["result"]["message"]=std::string(1025,'x');bool rejected=false;try{agent.tick();}catch(const std::exception&){rejected=true;}require(rejected&&h.count("activate")==activations&&h.acknowledgements==acks,"corrupt durable result was replayed/posted");pass("corrupt canonical/result fails before further side effects");}
 {auto s=aide_host::diagnostic("owned-secret https://localhost/#token=abcdef Bearer abcdef password=private\nnext","owned-secret");require(s.find("owned-secret")==s.npos&&s.find("abcdef")==s.npos&&s.find("private")==s.npos&&s.find('\n')==s.npos,"private diagnostic leaked");pass("token, URL, bearer and key/value diagnostics redacted before persistence");}
 std::cout<<"PASS native host production core "<<checks<<" checks\n";return 0;
}catch(const std::exception& e){std::cerr<<e.what()<<'\n';return 1;}}
