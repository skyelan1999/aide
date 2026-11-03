#pragma once
#include <wincrypt.h>
#include <webauthn.h>
#include <atomic>
namespace aide_desktop {
inline std::string encode(const BYTE* bytes,DWORD size) {
 DWORD count=0;if(!CryptBinaryToStringA(bytes,size,CRYPT_STRING_BASE64|CRYPT_STRING_NOCRLF,nullptr,&count))throw std::runtime_error("Encoding failed");
 std::string out(count,'\0');if(!CryptBinaryToStringA(bytes,size,CRYPT_STRING_BASE64|CRYPT_STRING_NOCRLF,out.data(),&count))throw std::runtime_error("Encoding failed");
 out.resize(count);while(!out.empty()&&(out.back()=='='||out.back()=='\0'))out.pop_back();for(auto& c:out){if(c=='+')c='-';else if(c=='/')c='_';}return out;
}
inline std::vector<BYTE> decode(const std::string& value) {
 if(value.empty()||value.size()>4096||!std::regex_match(value,std::regex("[A-Za-z0-9_-]+")))throw std::runtime_error("Invalid credential options");
 auto text=value;for(auto& c:text){if(c=='-')c='+';else if(c=='_')c='/';}while(text.size()%4)text+='=';
 DWORD count=0;if(!CryptStringToBinaryA(text.c_str(),DWORD(text.size()),CRYPT_STRING_BASE64,nullptr,&count,nullptr,nullptr))throw std::runtime_error("Invalid credential options");
 std::vector<BYTE> out(count);if(!CryptStringToBinaryA(text.c_str(),DWORD(text.size()),CRYPT_STRING_BASE64,out.data(),&count,nullptr,nullptr)||encode(out.data(),count)!=value)throw std::runtime_error("Invalid credential options");return out;
}
struct Hello {
 HMODULE library=LoadLibraryExW(L"webauthn.dll",nullptr,LOAD_LIBRARY_SEARCH_SYSTEM32);
 #define AIDE_HELLO_FN(name) decltype(&name) name=library?reinterpret_cast<decltype(&::name)>(GetProcAddress(library,#name)):nullptr
 AIDE_HELLO_FN(WebAuthNGetApiVersionNumber);
 AIDE_HELLO_FN(WebAuthNIsUserVerifyingPlatformAuthenticatorAvailable);
 AIDE_HELLO_FN(WebAuthNAuthenticatorMakeCredential);
 AIDE_HELLO_FN(WebAuthNAuthenticatorGetAssertion);
 AIDE_HELLO_FN(WebAuthNFreeCredentialAttestation);
 AIDE_HELLO_FN(WebAuthNFreeAssertion);
 AIDE_HELLO_FN(WebAuthNGetCancellationId);
 AIDE_HELLO_FN(WebAuthNGetErrorName);
 AIDE_HELLO_FN(WebAuthNCancelCurrentOperation);
 #undef AIDE_HELLO_FN
 // System library remains loaded for the lifetime of asynchronous OS ceremonies.
 bool available()const {BOOL ready=FALSE;return WebAuthNGetApiVersionNumber&&WebAuthNGetApiVersionNumber()>=2&&WebAuthNIsUserVerifyingPlatformAuthenticatorAvailable&&WebAuthNAuthenticatorMakeCredential&&WebAuthNAuthenticatorGetAssertion&&WebAuthNFreeCredentialAttestation&&WebAuthNFreeAssertion&&WebAuthNGetCancellationId&&WebAuthNCancelCurrentOperation&&SUCCEEDED(WebAuthNIsUserVerifyingPlatformAuthenticatorAvailable(&ready))&&ready;}
 static std::vector<std::string> ids(const fs::path& file) {
  std::vector<std::string> out;if(!fs::exists(file))return out;realPath(file);
  auto data=json::parse(read(file,65536),nullptr,false);if(!data.is_array()||data.size()>32)return out;
  for(auto& item:data){if(!item.is_string())continue;try{auto id=item.get<std::string>();if(decode(id).size()<=1024)out.push_back(id);}catch(...){}}
  return out;
 }
 json status(const fs::path& file)const {return json{{"available",available()},{"credentialIds",ids(file)}};}
 struct Operation {GUID cancellation{};std::atomic<bool> cancelled{false},done{false};bool userCancelled=false;};
 void failed(HRESULT code,const std::shared_ptr<Operation>& op)const{auto name=WebAuthNGetErrorName?WebAuthNGetErrorName(code):nullptr;op->userCancelled=name&&std::wstring(name)==L"NotAllowedError";}
 std::shared_ptr<Operation> begin()const {auto op=std::make_shared<Operation>();if(!available()||FAILED(WebAuthNGetCancellationId(&op->cancellation)))throw std::runtime_error("Windows Hello is unavailable; use the account password");return op;}
 void cancel(const std::shared_ptr<Operation>& op)const{if(op){op->cancelled=true;if(WebAuthNCancelCurrentOperation)WebAuthNCancelCurrentOperation(&op->cancellation);}}
 json perform(HWND owner,const std::string& operation,const json& options,const std::string& origin,const fs::path& file,const std::shared_ptr<Operation>& op)const {
  bool create=operation=="create";if(!create&&operation!="get")throw std::runtime_error("Invalid biometric request");
  auto rp=create?options.at("rp").at("id").get<std::string>():options.at("rpId").get<std::string>();
  auto challenge=options.at("challenge").get<std::string>();auto raw=decode(challenge);
  if(rp!="localhost"||raw.size()<16||raw.size()>64||op->cancelled)throw std::runtime_error("Invalid biometric request");
  auto client=json{{"type",create?"webauthn.create":"webauthn.get"},{"challenge",challenge},{"origin",origin},{"crossOrigin",false}}.dump();
  WEBAUTHN_CLIENT_DATA data{WEBAUTHN_CLIENT_DATA_CURRENT_VERSION,DWORD(client.size()),reinterpret_cast<PBYTE>(client.data()),WEBAUTHN_HASH_ALGORITHM_SHA_256};
  auto list=options.value(create?"excludeCredentials":"allowCredentials",json::array());
  if(!list.is_array()||list.size()>128||(!create&&list.empty()))throw std::runtime_error("Invalid credential list");
  std::vector<std::vector<BYTE>> decoded;decoded.reserve(list.size());std::vector<WEBAUTHN_CREDENTIAL> credentials;credentials.reserve(list.size());
  auto own=ids(file);
  for(auto& descriptor:list){if(descriptor.value("type","")!="public-key")throw std::runtime_error("Invalid credential type");auto id=descriptor.at("id").get<std::string>();auto bytes=decode(id);if(bytes.empty()||bytes.size()>1024)throw std::runtime_error("Invalid credential ID");
   if(create||std::find(own.begin(),own.end(),id)!=own.end()){decoded.push_back(std::move(bytes));auto& b=decoded.back();credentials.push_back({WEBAUTHN_CREDENTIAL_CURRENT_VERSION,DWORD(b.size()),b.data(),WEBAUTHN_CREDENTIAL_TYPE_PUBLIC_KEY});}}
  if(!create&&credentials.empty())throw std::runtime_error("Bind Windows Hello in Settings > Account first");
  WEBAUTHN_CREDENTIALS allow{DWORD(credentials.size()),credentials.data()};
  if(create){
   auto user=decode(options.at("user").at("id").get<std::string>());bool algorithm=false;
   for(auto& entry:options.at("pubKeyCredParams"))if(entry.value("type","")=="public-key"&&entry.value("alg",0)==-7)algorithm=true;
   if(!algorithm||user.empty()||user.size()>64||options.value("attestation","none")!="none"||options.value("authenticatorSelection",json::object()).value("residentKey","")=="required"||own.size()>=32)throw std::runtime_error("Unsupported registration options");
   WEBAUTHN_RP_ENTITY_INFORMATION party{WEBAUTHN_RP_ENTITY_INFORMATION_CURRENT_VERSION,L"localhost",L"Aide",nullptr};
   WEBAUTHN_USER_ENTITY_INFORMATION account{WEBAUTHN_USER_ENTITY_INFORMATION_CURRENT_VERSION,DWORD(user.size()),user.data(),L"aide-local",nullptr,L"Aide local"};
   WEBAUTHN_COSE_CREDENTIAL_PARAMETER parameter{WEBAUTHN_COSE_CREDENTIAL_PARAMETER_CURRENT_VERSION,WEBAUTHN_CREDENTIAL_TYPE_PUBLIC_KEY,-7};
   WEBAUTHN_COSE_CREDENTIAL_PARAMETERS parameters{1,&parameter};
   WEBAUTHN_AUTHENTICATOR_MAKE_CREDENTIAL_OPTIONS settings{};settings.dwVersion=WEBAUTHN_AUTHENTICATOR_MAKE_CREDENTIAL_OPTIONS_VERSION_2;settings.dwTimeoutMilliseconds=120000;settings.CredentialList=allow;settings.dwAuthenticatorAttachment=WEBAUTHN_AUTHENTICATOR_ATTACHMENT_PLATFORM;settings.dwUserVerificationRequirement=WEBAUTHN_USER_VERIFICATION_REQUIREMENT_REQUIRED;settings.dwAttestationConveyancePreference=WEBAUTHN_ATTESTATION_CONVEYANCE_PREFERENCE_NONE;settings.pCancellationId=&op->cancellation;
   PWEBAUTHN_CREDENTIAL_ATTESTATION result=nullptr;auto code=WebAuthNAuthenticatorMakeCredential(owner,&party,&account,&parameters,&data,&settings,&result);
   if(FAILED(code)||!result){if(result)WebAuthNFreeCredentialAttestation(result);failed(code,op);throw std::runtime_error("Windows Hello registration cancelled or failed");}
   json response;try{auto id=encode(result->pbCredentialId,result->cbCredentialId);response={{"id",id},{"rawId",id},{"type","public-key"},{"response",{{"attestationObject",encode(result->pbAttestationObject,result->cbAttestationObject)},{"clientDataJSON",encode(reinterpret_cast<const BYTE*>(client.data()),DWORD(client.size()))},{"transports",{"internal"}}}}};
    if(!op->cancelled){own.push_back(id);write(file,json(own).dump());}
   }catch(...){WebAuthNFreeCredentialAttestation(result);throw;}WebAuthNFreeCredentialAttestation(result);if(op->cancelled)throw std::runtime_error("Cancelled");return response;
  }
  WEBAUTHN_AUTHENTICATOR_GET_ASSERTION_OPTIONS settings{};settings.dwVersion=WEBAUTHN_AUTHENTICATOR_GET_ASSERTION_OPTIONS_VERSION_3;settings.dwTimeoutMilliseconds=120000;settings.CredentialList=allow;settings.dwAuthenticatorAttachment=WEBAUTHN_AUTHENTICATOR_ATTACHMENT_PLATFORM;settings.dwUserVerificationRequirement=WEBAUTHN_USER_VERIFICATION_REQUIREMENT_REQUIRED;settings.pCancellationId=&op->cancellation;
  PWEBAUTHN_ASSERTION result=nullptr;auto code=WebAuthNAuthenticatorGetAssertion(owner,L"localhost",&data,&settings,&result);
  if(FAILED(code)||!result){if(result)WebAuthNFreeAssertion(result);failed(code,op);throw std::runtime_error("Windows Hello verification cancelled or failed");}
  json response;try{auto id=encode(result->Credential.pbId,result->Credential.cbId);response={{"id",id},{"rawId",id},{"type","public-key"},{"response",{{"clientDataJSON",encode(reinterpret_cast<const BYTE*>(client.data()),DWORD(client.size()))},{"authenticatorData",encode(result->pbAuthenticatorData,result->cbAuthenticatorData)},{"signature",encode(result->pbSignature,result->cbSignature)},{"userHandle",result->cbUserId?json(encode(result->pbUserId,result->cbUserId)):json(nullptr)}}}};}catch(...){WebAuthNFreeAssertion(result);throw;}WebAuthNFreeAssertion(result);if(op->cancelled)throw std::runtime_error("Cancelled");return response;
 }
};
}
