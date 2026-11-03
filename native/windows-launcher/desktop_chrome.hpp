#pragma once
#include <dwmapi.h>
namespace aide_desktop {
struct Chrome {
 COLORREF background=RGB(32,35,40),surface=RGB(36,40,46),selected=RGB(52,66,82),text=RGB(224,228,235),muted=RGB(164,173,185),line=RGB(55,60,68);
 HFONT font=nullptr;bool dark=true;
 Chrome(){HDC dc=GetDC(nullptr);int dpi=GetDeviceCaps(dc,LOGPIXELSY);ReleaseDC(nullptr,dc);font=CreateFontW(-MulDiv(10,dpi,72),0,0,0,FW_NORMAL,FALSE,FALSE,FALSE,DEFAULT_CHARSET,OUT_DEFAULT_PRECIS,CLIP_DEFAULT_PRECIS,CLEARTYPE_QUALITY,DEFAULT_PITCH,L"Segoe UI");}
 ~Chrome(){if(font)DeleteObject(font);}
 static COLORREF color(const std::string& value){if(!std::regex_match(value,std::regex("#[0-9a-fA-F]{6}")))throw std::runtime_error("Invalid native appearance color");auto n=std::stoul(value.substr(1),nullptr,16);return RGB((n>>16)&255,(n>>8)&255,n&255);}
 void apply(HWND window,HWND tabs,const json& data){
  if(!data.is_object()||data.size()!=7||!data.at("dark").is_boolean())throw std::runtime_error("Invalid native appearance");
  auto bg=color(data.at("background").get<std::string>()),s=color(data.at("surface").get<std::string>()),sel=color(data.at("selected").get<std::string>()),fg=color(data.at("text").get<std::string>()),m=color(data.at("muted").get<std::string>()),l=color(data.at("line").get<std::string>());
  background=bg;surface=s;selected=sel;text=fg;muted=m;line=l;dark=data.at("dark").get<bool>();BOOL dim=dark;DwmSetWindowAttribute(window,20,&dim,sizeof(dim));InvalidateRect(window,nullptr,TRUE);InvalidateRect(tabs,nullptr,TRUE);InvalidateRect(GetDlgItem(window,101),nullptr,TRUE);InvalidateRect(GetDlgItem(window,102),nullptr,TRUE);
 }
 static void fill(HDC dc,const RECT& r,COLORREF color){auto brush=CreateSolidBrush(color);FillRect(dc,&r,brush);DeleteObject(brush);}
 void rounded(HDC dc,RECT r,COLORREF color){auto brush=CreateSolidBrush(color);auto pen=CreatePen(PS_SOLID,1,color);auto oldBrush=SelectObject(dc,brush),oldPen=SelectObject(dc,pen);RoundRect(dc,r.left,r.top,r.right,r.bottom,12,12);SelectObject(dc,oldBrush);SelectObject(dc,oldPen);DeleteObject(brush);DeleteObject(pen);}
 void label(HDC dc,RECT r,const wchar_t* title,COLORREF color,UINT format){auto previous=SelectObject(dc,font);SetBkMode(dc,TRANSPARENT);SetTextColor(dc,color);DrawTextW(dc,title,-1,&r,format|DT_SINGLELINE|DT_VCENTER|DT_END_ELLIPSIS|DT_NOPREFIX);SelectObject(dc,previous);}
 void button(const DRAWITEMSTRUCT& item){fill(item.hDC,item.rcItem,background);RECT r=item.rcItem;InflateRect(&r,-3,-4);rounded(item.hDC,r,(item.itemState&ODS_SELECTED)?selected:surface);label(item.hDC,r,item.CtlID==101?L"+":L"×",text,DT_CENTER);}
 static LRESULT CALLBACK tabsProcedure(HWND hwnd,UINT message,WPARAM w,LPARAM l,UINT_PTR,DWORD_PTR pointer){
  auto self=reinterpret_cast<Chrome*>(pointer);
  if(message==WM_ERASEBKGND)return TRUE;
  if(message==WM_PAINT){PAINTSTRUCT paint{};HDC dc=BeginPaint(hwnd,&paint);RECT bounds{};GetClientRect(hwnd,&bounds);fill(dc,bounds,self->background);
   for(int i=0;i<TabCtrl_GetItemCount(hwnd);i++){RECT r{};TabCtrl_GetItemRect(hwnd,i,&r);r.top=4;r.bottom=bounds.bottom-4;InflateRect(&r,-3,0);bool active=i==TabCtrl_GetCurSel(hwnd);self->rounded(dc,r,active?self->selected:self->surface);wchar_t title[256]{};TCITEMW item{};item.mask=TCIF_TEXT;item.pszText=title;item.cchTextMax=256;TabCtrl_GetItem(hwnd,i,&item);InflateRect(&r,-14,0);self->label(dc,r,title,active?self->text:self->muted,DT_LEFT);}
   EndPaint(hwnd,&paint);return 0;}
  return DefSubclassProc(hwnd,message,w,l);
 }
 void install(HWND tabs){SendMessageW(tabs,WM_SETFONT,reinterpret_cast<WPARAM>(font),FALSE);TabCtrl_SetItemSize(tabs,230,44);SetWindowSubclass(tabs,tabsProcedure,1,reinterpret_cast<DWORD_PTR>(this));}
};
}
