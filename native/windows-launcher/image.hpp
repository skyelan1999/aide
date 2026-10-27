#pragma once
#include <regex>
#include <set>
#include <sstream>
#include <stdexcept>
#include <string>
#include "third_party/json.hpp"
namespace aide_image {
inline std::string field(const nlohmann::json& value,const std::string& key){if(!value.is_object()||!value.contains(key)||!value[key].is_string())return {};return value[key].get<std::string>();}
inline std::string label(const nlohmann::json& value,const std::string& key){if(!value.is_object()||!value.contains("Config")||!value["Config"].is_object()||!value["Config"].contains("Labels"))return {};return field(value["Config"]["Labels"],key);}
// The caller must resolve the pinned immutable Docker reference, or the sole
// receipt of its successful SHA256-verified load. Never inspect unrelated tags.
inline std::string canonical(const nlohmann::json& value,const std::string& platform,const std::string& source){
 auto id=field(value,"Id");if(!std::regex_match(id,std::regex("sha256:[a-f0-9]{64}")))throw std::runtime_error("Pinned runtime canonical identity unavailable");
 if(field(value,"Os")+"/"+field(value,"Architecture")!=platform)throw std::runtime_error("Pinned runtime platform mismatch");
 if(label(value,"aide.srcsha")!=source)throw std::runtime_error("Pinned runtime source fingerprint mismatch");
 if(label(value,"aide.cad-runtime")!="8.0.1.1.0")throw std::runtime_error("Pinned legacy runtime CAD mismatch");return id;
}
inline std::string receipt(const std::string& output){
 std::set<std::string> references;std::istringstream in(output);std::string line;
 while(std::getline(in,line)){if(!line.empty()&&line.back()=='\r')line.pop_back();std::smatch match;if(std::regex_match(line,match,std::regex("Loaded image(?: ID)?: ([A-Za-z0-9][A-Za-z0-9._:/@-]*)[ \\t]*")))references.insert(match[1].str());}
 if(references.size()!=1)throw std::runtime_error("Cannot uniquely resolve this verified image load");return *references.begin();
}
}
