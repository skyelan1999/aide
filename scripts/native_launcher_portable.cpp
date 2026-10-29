// Test executable for the exact portable archive/image code used by Win32.
#include "../native/windows-launcher/archive.hpp"
#include "../native/windows-launcher/image.hpp"
#include "../native/windows-launcher/source_inputs.hpp"
#include "../native/windows-launcher/installation.hpp"
#include <iostream>
int main(int argc,char** argv){
 try{
  if(argc==4&&std::string(argv[1])=="extract"){
   if(std::filesystem::exists(argv[3]))throw std::runtime_error("Destination already exists");
   std::ofstream output(argv[3],std::ios::binary);if(!output)throw std::runtime_error("Cannot open test destination");
   aide_archive::extract(argv[2],[&](const unsigned char* bytes,size_t count){output.write(reinterpret_cast<const char*>(bytes),count);if(!output)throw std::runtime_error("Output write failed");});return 0;
  }
  if(argc==2&&std::string(argv[1])=="identity"){
   auto input=nlohmann::json::parse(std::cin);std::cout<<aide_image::canonical(input["metadata"],input["platform"],input["source"]);return 0;
  }
  if(argc==2&&std::string(argv[1])=="receipt"){std::string input((std::istreambuf_iterator<char>(std::cin)),{});std::cout<<aide_image::receipt(input);return 0;}
  if(argc==3&&std::string(argv[1])=="source-inputs"){auto manifest=nlohmann::json::parse(std::cin);std::cout<<nlohmann::json(aide_source::inputs(argv[2],manifest));return 0;}
  if(argc==4&&std::string(argv[1])=="same-install")return aide_installation::owns(argv[2],argv[3])?0:1;
  throw std::runtime_error("Unknown test action");
 }catch(const std::exception& e){std::cerr<<e.what()<<'\n';return 1;}
}
