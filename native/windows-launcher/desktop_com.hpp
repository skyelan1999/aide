#pragma once
#include <atomic>
#include <functional>
namespace aide_desktop {
template<class I> const IID& iid(){return __uuidof(I);}
template<> inline const IID& iid<ICoreWebView2_14>(){return IID_ICoreWebView2_14;}
template<> inline const IID& iid<ICoreWebView2_4>(){return IID_ICoreWebView2_4;}
template<> inline const IID& iid<ICoreWebView2NavigationStartingEventHandler>(){return IID_ICoreWebView2NavigationStartingEventHandler;}
template<> inline const IID& iid<ICoreWebView2DocumentTitleChangedEventHandler>(){return IID_ICoreWebView2DocumentTitleChangedEventHandler;}
template<> inline const IID& iid<ICoreWebView2NewWindowRequestedEventHandler>(){return IID_ICoreWebView2NewWindowRequestedEventHandler;}
template<> inline const IID& iid<ICoreWebView2WindowCloseRequestedEventHandler>(){return IID_ICoreWebView2WindowCloseRequestedEventHandler;}
template<> inline const IID& iid<ICoreWebView2PermissionRequestedEventHandler>(){return IID_ICoreWebView2PermissionRequestedEventHandler;}
template<> inline const IID& iid<ICoreWebView2WebMessageReceivedEventHandler>(){return IID_ICoreWebView2WebMessageReceivedEventHandler;}
template<> inline const IID& iid<ICoreWebView2ServerCertificateErrorDetectedEventHandler>(){return IID_ICoreWebView2ServerCertificateErrorDetectedEventHandler;}
template<> inline const IID& iid<ICoreWebView2DownloadStartingEventHandler>(){return IID_ICoreWebView2DownloadStartingEventHandler;}
template<> inline const IID& iid<ICoreWebView2CreateCoreWebView2ControllerCompletedHandler>(){return IID_ICoreWebView2CreateCoreWebView2ControllerCompletedHandler;}
template<> inline const IID& iid<ICoreWebView2ExecuteScriptCompletedHandler>(){return IID_ICoreWebView2ExecuteScriptCompletedHandler;}
template<> inline const IID& iid<ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler>(){return IID_ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler;}
template<> inline const IID& iid<ICoreWebView2AcceleratorKeyPressedEventHandler>(){return IID_ICoreWebView2AcceleratorKeyPressedEventHandler;}
template<> inline const IID& iid<ICoreWebView2AddScriptToExecuteOnDocumentCreatedCompletedHandler>(){return IID_ICoreWebView2AddScriptToExecuteOnDocumentCreatedCompletedHandler;}
template<class T> class Ptr {
 T* p=nullptr;
public:
 Ptr()=default;explicit Ptr(T* v):p(v){if(p)p->AddRef();}
 Ptr(const Ptr& o):Ptr(o.p){} Ptr(Ptr&& o)noexcept:p(o.p){o.p=nullptr;}
 ~Ptr(){if(p)p->Release();}
 Ptr& operator=(Ptr o){std::swap(p,o.p);return *this;}
 T* get()const{return p;} T* operator->()const{return p;} explicit operator bool()const{return p!=nullptr;}
 T** put(){if(p)p->Release();p=nullptr;return &p;}
 template<class I> Ptr<I> as()const{Ptr<I> out;if(p)p->QueryInterface(iid<I>(),reinterpret_cast<void**>(out.put()));return out;}
};
template<class I,class... A> class Handler final:public I {
 std::atomic<ULONG> refs{1};std::function<HRESULT(A...)> fn;
public:
 explicit Handler(std::function<HRESULT(A...)> f):fn(std::move(f)){}
 HRESULT STDMETHODCALLTYPE QueryInterface(REFIID id,void** out)override{
  if(!out)return E_POINTER;*out=nullptr;if(id==IID_IUnknown||id==iid<I>()){*out=static_cast<I*>(this);AddRef();return S_OK;}return E_NOINTERFACE;
 }
 ULONG STDMETHODCALLTYPE AddRef()override{return ++refs;}
 ULONG STDMETHODCALLTYPE Release()override{auto n=--refs;if(!n)delete this;return n;}
 HRESULT STDMETHODCALLTYPE Invoke(A... a)override{try{return fn(a...);}catch(...){return E_FAIL;}}
};
template<class I,class... A,class F> Ptr<I> callback(F f){Ptr<I> result;*result.put()=new Handler<I,A...>(f);return result;}
inline std::wstring takeString(LPWSTR value){std::wstring result=value?value:L"";CoTaskMemFree(value);return result;}
}
