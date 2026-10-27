#pragma once
// Streaming reader for the fixed image member in our SHA256-verified bundles.
// No archive path is ever used as a host destination.
#include <array>
#include <algorithm>
#include <cstdint>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <functional>
#include <limits>
#include <map>
#include <stdexcept>
#include <string>
#include "third_party/miniz/miniz.h"
namespace aide_archive {
inline uint32_t crc(uint32_t value,const unsigned char* bytes,size_t count) {
 static const auto table=[] {std::array<uint32_t,256> t{};for(uint32_t i=0;i<256;i++){auto n=i;for(int j=0;j<8;j++)n=(n>>1)^((n&1)?0xedb88320:0);t[i]=n;}return t;}();
 for(size_t i=0;i<count;i++)value=table[(value^bytes[i])&255]^(value>>8);return value;
}
class Gzip {
 std::ifstream file;
 std::array<unsigned char,65536> input{};
 std::array<unsigned char,TINFL_LZ_DICT_SIZE> output{};
 size_t pos=0,available=0,offset=0,start=0,count=0;
 tinfl_decompressor decoder{};
 uint32_t checksum=~uint32_t(0),total=0;
 bool finished=false;
 unsigned char byte() {
  if(pos==available){file.read(reinterpret_cast<char*>(input.data()),input.size());available=size_t(file.gcount());pos=0;if(!available)throw std::runtime_error("Truncated gzip data");}
  return input[pos++];
 }
 void next() {
  if(finished)return;
  if(pos==available){file.read(reinterpret_cast<char*>(input.data()),input.size());available=size_t(file.gcount());pos=0;}
  size_t in=available-pos,out=output.size()-offset;
  auto status=tinfl_decompress(&decoder,input.data()+pos,&in,output.data(),output.data()+offset,&out,TINFL_FLAG_HAS_MORE_INPUT);
  pos+=in;start=offset;count=out;offset=(offset+out)&(output.size()-1);
  checksum=crc(checksum,output.data()+start,out);total+=uint32_t(out);
  if(status<TINFL_STATUS_DONE||(!in&&!out&&status!=TINFL_STATUS_DONE))throw std::runtime_error("Invalid or truncated gzip stream");
  if(status==TINFL_STATUS_DONE){
   uint32_t expected=0,size=0;for(int i=0;i<4;i++)expected|=uint32_t(byte())<<(i*8);for(int i=0;i<4;i++)size|=uint32_t(byte())<<(i*8);
   if(expected!=~checksum||size!=total)throw std::runtime_error("Gzip checksum/size mismatch");
   // Release bundles use one gzip member; reject ambiguous appended streams.
   while(pos<available)if(input[pos++]!=0)throw std::runtime_error("Unexpected trailing gzip data");
   char c;while(file.get(c))if(c!=0)throw std::runtime_error("Unexpected trailing gzip data");
   if(file.bad())throw std::runtime_error("Cannot read gzip stream");finished=true;
  }
 }
public:
 explicit Gzip(const std::filesystem::path& path):file(path,std::ios::binary) {
  if(!file)throw std::runtime_error("Cannot read bundle archive");
  uint32_t header=~uint32_t(0);size_t bytes=0;
  auto get=[&] {if(++bytes>65536)throw std::runtime_error("Gzip header exceeds limit");auto b=byte();header=crc(header,&b,1);return b;};
  if(get()!=31||get()!=139||get()!=8)throw std::runtime_error("Invalid gzip header");auto flags=get();if(flags&224)throw std::runtime_error("Invalid gzip flags");for(int i=0;i<6;i++)get();
  if(flags&4){unsigned size=get();size|=unsigned(get())<<8;while(size--)get();}
  if(flags&8)while(get()){}if(flags&16)while(get()){}
  if(flags&2){unsigned expected=byte();expected|=unsigned(byte())<<8;if(expected!=((~header)&65535))throw std::runtime_error("Gzip header checksum mismatch");}
  tinfl_init(&decoder);
 }
 size_t read(unsigned char* dest,size_t size) {
  size_t done=0;
  while(done<size){if(!count){if(finished)break;next();if(!count)continue;}auto n=std::min(count,size-done);std::memcpy(dest+done,output.data()+start,n);done+=n;start+=n;count-=n;}
  return done;
 }
 void exact(unsigned char* dest,size_t size){if(read(dest,size)!=size)throw std::runtime_error("Truncated tar data");}
 void drain(){std::array<unsigned char,65536> b{};while(read(b.data(),b.size())){}}
};
inline uint64_t number(const unsigned char* p,size_t n) {
 uint64_t result=0;
 if(p[0]&128){if(p[0]&64)throw std::runtime_error("Negative tar size");result=p[0]&63;for(size_t i=1;i<n;i++){if(result>(std::numeric_limits<uint64_t>::max()-p[i])/256)throw std::runtime_error("Tar size overflow");result=result*256+p[i];}return result;}
 size_t a=0,b=n;while(a<b&&(p[a]==' '||!p[a]))a++;while(b>a&&(p[b-1]==' '||!p[b-1]))b--;
 for(size_t i=a;i<b;i++){if(p[i]<'0'||p[i]>'7'||result>(std::numeric_limits<uint64_t>::max()-7)/8)throw std::runtime_error("Invalid tar number");result=result*8+p[i]-'0';}return result;
}
inline std::string text(const unsigned char* p,size_t n){size_t size=0;while(size<n&&p[size])size++;return std::string(reinterpret_cast<const char*>(p),size);}
inline uint64_t decimal(const std::string& s){uint64_t n=0;if(s.empty())throw std::runtime_error("Invalid PAX number");for(unsigned char c:s){if(c<'0'||c>'9'||n>(std::numeric_limits<uint64_t>::max()-(c-'0'))/10)throw std::runtime_error("Invalid PAX number");n=n*10+c-'0';}return n;}
inline void pax(const std::string& data,std::map<std::string,std::string>& fields) {
 for(size_t i=0;i<data.size();){auto space=data.find(' ',i);if(space==data.npos||space-i>8)throw std::runtime_error("Invalid PAX record");auto n=decimal(data.substr(i,space-i));if(n<=space-i+2||n>data.size()-i||data[i+size_t(n)-1]!='\n')throw std::runtime_error("Invalid PAX length");auto line=data.substr(space+1,i+size_t(n)-space-2);auto eq=line.find('=');if(eq==0||eq==line.npos)throw std::runtime_error("Invalid PAX attribute");auto key=line.substr(0,eq);if(key=="path"||key=="size")fields[key]=line.substr(eq+1);i+=size_t(n);}
}
inline void extract(const std::filesystem::path& archive,const std::function<void(const unsigned char*,size_t)>& sink) {
 Gzip input(archive);std::array<unsigned char,512> header{};std::array<unsigned char,65536> buffer{};
 std::map<std::string,std::string> global,next;bool found=false,ended=false;
 while(true){auto got=input.read(header.data(),header.size());if(!got)break;if(got!=header.size())throw std::runtime_error("Truncated tar header");
  uint64_t sum=0,raw=0;for(size_t i=0;i<header.size();i++){raw+=header[i];sum+=(i>=148&&i<156)?32:header[i];}if(!raw){ended=true;break;}
  if(number(header.data()+148,8)!=sum)throw std::runtime_error("Tar header checksum mismatch");
  auto size=number(header.data()+124,12);auto name=text(header.data(),100);auto type=header[156];
  if(std::memcmp(header.data()+257,"ustar\0",6)==0){auto prefix=text(header.data()+345,155);if(!prefix.empty())name=prefix+"/"+name;}
  bool meta=type=='x'||type=='g'||type=='L'||type=='K';
  if(meta){if(size>65536)throw std::runtime_error("Tar metadata exceeds 64KiB");std::string data(size,'\0');input.exact(reinterpret_cast<unsigned char*>(data.data()),data.size());if(type=='L'){while(!data.empty()&&(data.back()==0||data.back()=='\n'))data.pop_back();next["path"]=data;}else if(type=='x'||type=='g')pax(data,type=='g'?global:next);}
  else{
   for(const auto* fields:{&global,&next}){auto path=fields->find("path"),length=fields->find("size");if(path!=fields->end())name=path->second;if(length!=fields->end())size=decimal(length->second);}next.clear();
   while(name.rfind("./",0)==0)name.erase(0,2);bool wanted=name=="aide-offline/docker-images/aide-local.tar";
   if(wanted&&(found||(type!=0&&type!='0')))throw std::runtime_error("Image member must be unique and regular");
   uint64_t left=size;while(left){auto n=size_t(std::min<uint64_t>(left,buffer.size()));input.exact(buffer.data(),n);if(wanted)sink(buffer.data(),n);left-=n;}
   if(wanted)found=true;
  }
  auto padding=size_t((512-size%512)%512);if(padding)input.exact(buffer.data(),padding);
 }
 input.drain();if(!ended||!found)throw std::runtime_error("Image member or tar terminator missing");
}
} // namespace aide_archive
