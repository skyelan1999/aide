#pragma once
#include <filesystem>
#include <string>
namespace aide_installation {
inline bool owns(const std::filesystem::path& root,const std::string& reported){
 if(reported.empty())return false;
 try{std::error_code error;bool same=std::filesystem::equivalent(root,std::filesystem::u8path(reported),error);return !error&&same;}catch(...){return false;}
}
}
