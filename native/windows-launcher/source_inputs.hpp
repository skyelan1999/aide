#pragma once
#include <algorithm>
#include <filesystem>
#include <string>
#include <vector>
#include "third_party/json.hpp"
namespace aide_source {
inline std::vector<std::string> inputs(const std::filesystem::path& root,const nlohmann::json& manifest){
 namespace fs=std::filesystem;std::vector<std::string> names;
 for(const auto& group:{std::string("filteredTrees"),std::string("plainTrees")})for(const auto& item:manifest.at(group)){
  auto tree=root/fs::u8path(item.get<std::string>());if(!fs::exists(tree))continue;
  for(auto it=fs::recursive_directory_iterator(tree);it!=fs::recursive_directory_iterator();++it){
   auto name=it->path().filename().u8string();bool filtered=group=="filteredTrees";
   if(filtered&&it->is_directory()&&(name=="node_modules"||name=="__pycache__")){it.disable_recursion_pending();continue;}
   if(it->is_regular_file()&&!it->is_symlink()&&(!filtered||(name!=".DS_Store"&&it->path().extension()!=L".pyc")))names.push_back(it->path().lexically_relative(root).generic_u8string());
  }
 }
 // Keep duplicate explicit inputs: the POSIX sort intentionally does not use -u.
 for(const auto& item:manifest.at("files")){auto name=item.get<std::string>();if(fs::is_regular_file(root/fs::u8path(name)))names.push_back(name);}
 std::sort(names.begin(),names.end());return names;
}
}
