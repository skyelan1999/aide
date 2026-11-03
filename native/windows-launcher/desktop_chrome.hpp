#pragma once
#include <dwmapi.h>
#include <cmath>
namespace aide_desktop {
struct Chrome {
 COLORREF background=RGB(32,35,40),surface=RGB(36,40,46),selected=RGB(52,66,82),text=RGB(224,228,235),muted=RGB(164,173,185),line=RGB(55,60,68);
 HFONT font=nullptr;bool dark=true;UINT dpi=96;int hovered=-1;HWND hoveredButton=nullptr;UINT appliedDpi=0;int lastWidth=-1,lastCount=-1,lastSelected=-2,lastItem=-1;
 Chrome(){setDpi(96);}
 ~Chrome(){if(font)DeleteObject(font);}
 int px(int dip)const{return MulDiv(dip,int(dpi),96);}
 int height()const{return px(32);}
 void setDpi(UINT value){dpi=value>=72&&value<=768?value:96;auto next=CreateFontW(-MulDiv(9,int(dpi),72),0,0,0,FW_NORMAL,FALSE,FALSE,FALSE,DEFAULT_CHARSET,OUT_DEFAULT_PRECIS,CLIP_DEFAULT_PRECIS,DEFAULT_QUALITY,DEFAULT_PITCH,L"Segoe UI");if(next){auto previous=font;font=next;if(previous)DeleteObject(previous);}}
 static COLORREF color(const std::string& value){if(!std::regex_match(value,std::regex("#[0-9a-fA-F]{6}")))throw std::runtime_error("Invalid native appearance color");auto n=std::stoul(value.substr(1),nullptr,16);return RGB((n>>16)&255,(n>>8)&255,n&255);}
 static double luminance(COLORREF value){auto channel=[](int v){double s=v/255.0;return s<=.04045?s/12.92:std::pow((s+.055)/1.055,2.4);};return .2126*channel(GetRValue(value))+.7152*channel(GetGValue(value))+.0722*channel(GetBValue(value));}
 static double contrast(COLORREF a,COLORREF b){auto x=luminance(a),y=luminance(b);return (std::max(x,y)+.05)/(std::min(x,y)+.05);}
 static COLORREF readable(COLORREF ink,COLORREF bg,double ratio=4.5){if(contrast(ink,bg)>=ratio)return ink;return contrast(RGB(255,255,255),bg)>contrast(RGB(0,0,0),bg)?RGB(255,255,255):RGB(0,0,0);}
 COLORREF accent()const{return readable(dark?RGB(138,191,255):RGB(0,100,210),selected,3.0);}
 void apply(HWND window,HWND tabs,const json& data){
  if(!data.is_object()||data.size()!=7||!data.at("dark").is_boolean())throw std::runtime_error("Invalid native appearance");
  auto bg=color(data.at("background").get<std::string>()),s=color(data.at("surface").get<std::string>()),sel=color(data.at("selected").get<std::string>()),fg=color(data.at("text").get<std::string>()),m=color(data.at("muted").get<std::string>()),l=color(data.at("line").get<std::string>());
  background=bg;surface=s;selected=sel;text=fg;muted=m;line=l;dark=data.at("dark").get<bool>();BOOL dim=dark;DwmSetWindowAttribute(window,20,&dim,sizeof(dim));RedrawWindow(window,nullptr,nullptr,RDW_INVALIDATE|RDW_ERASE|RDW_FRAME|RDW_UPDATENOW|RDW_ALLCHILDREN);
 }
 static void fill(HDC dc,const RECT& r,COLORREF color){auto brush=CreateSolidBrush(color);FillRect(dc,&r,brush);DeleteObject(brush);}
 void rounded(HDC dc,RECT r,COLORREF color){auto brush=CreateSolidBrush(color);auto pen=CreatePen(PS_SOLID,1,color);auto oldBrush=SelectObject(dc,brush),oldPen=SelectObject(dc,pen);RoundRect(dc,r.left,r.top,r.right,r.bottom,px(6),px(6));SelectObject(dc,oldBrush);SelectObject(dc,oldPen);DeleteObject(brush);DeleteObject(pen);}
 void label(HDC dc,RECT r,const wchar_t* title,COLORREF color,UINT format){auto previous=SelectObject(dc,font);SetBkMode(dc,TRANSPARENT);SetTextColor(dc,color);DrawTextW(dc,title,-1,&r,format|DT_SINGLELINE|DT_VCENTER|DT_END_ELLIPSIS|DT_NOPREFIX);SelectObject(dc,previous);}
 void button(const DRAWITEMSTRUCT& item){auto base=item.CtlID==102?selected:background;fill(item.hDC,item.rcItem,base);RECT r=item.rcItem;InflateRect(&r,-px(2),-px(2));if((item.itemState&ODS_SELECTED)||hoveredButton==item.hwndItem)rounded(item.hDC,r,item.CtlID==102?surface:selected);
  auto pen=CreatePen(PS_SOLID,std::max(1,px(1)),readable(text,base));auto previous=SelectObject(item.hDC,pen);int x=(r.left+r.right)/2,y=(r.top+r.bottom)/2,d=px(4);
  if(item.CtlID==101){MoveToEx(item.hDC,x-d,y,nullptr);LineTo(item.hDC,x+d+1,y);MoveToEx(item.hDC,x,y-d,nullptr);LineTo(item.hDC,x,y+d+1);}else{MoveToEx(item.hDC,x-d,y-d,nullptr);LineTo(item.hDC,x+d+1,y+d+1);MoveToEx(item.hDC,x+d,y-d,nullptr);LineTo(item.hDC,x-d-1,y+d+1);}SelectObject(item.hDC,previous);DeleteObject(pen);if(item.itemState&ODS_FOCUS)DrawFocusRect(item.hDC,&r);
 }
 static LRESULT CALLBACK buttonProcedure(HWND hwnd,UINT message,WPARAM w,LPARAM l,UINT_PTR,DWORD_PTR pointer){auto self=reinterpret_cast<Chrome*>(pointer);
  if(message==WM_ERASEBKGND)return TRUE;
  if(message==WM_PAINT){PAINTSTRUCT paint{};DRAWITEMSTRUCT item{};item.hDC=BeginPaint(hwnd,&paint);item.hwndItem=hwnd;item.CtlType=ODT_BUTTON;item.CtlID=GetDlgCtrlID(hwnd);GetClientRect(hwnd,&item.rcItem);if(GetFocus()==hwnd)item.itemState|=ODS_FOCUS;if(SendMessageW(hwnd,BM_GETSTATE,0,0)&BST_PUSHED)item.itemState|=ODS_SELECTED;self->button(item);EndPaint(hwnd,&paint);return 0;}
  if(message==WM_MOUSEMOVE){if(self->hoveredButton!=hwnd){self->hoveredButton=hwnd;InvalidateRect(hwnd,nullptr,FALSE);TRACKMOUSEEVENT track{sizeof(track),TME_LEAVE,hwnd,0};TrackMouseEvent(&track);}}else if(message==WM_MOUSELEAVE){self->hoveredButton=nullptr;InvalidateRect(hwnd,nullptr,FALSE);}return DefSubclassProc(hwnd,message,w,l);
 }
 static LRESULT CALLBACK scrollerProcedure(HWND hwnd,UINT message,WPARAM w,LPARAM l,UINT_PTR,DWORD_PTR pointer){auto self=reinterpret_cast<Chrome*>(pointer);if(message==WM_ERASEBKGND)return TRUE;if(message==WM_PAINT){PAINTSTRUCT paint{};HDC dc=BeginPaint(hwnd,&paint);RECT r{};GetClientRect(hwnd,&r);fill(dc,r,self->background);auto pen=CreatePen(PS_SOLID,std::max(1,self->px(1)),readable(self->text,self->background));auto old=SelectObject(dc,pen);int mid=(r.top+r.bottom)/2,d=self->px(3);for(int direction:{-1,1}){int x=(direction<0?r.right/4:3*r.right/4);MoveToEx(dc,x-direction*d,mid-d,nullptr);LineTo(dc,x,mid);LineTo(dc,x-direction*d,mid+d);}SelectObject(dc,old);DeleteObject(pen);EndPaint(hwnd,&paint);return 0;}return DefSubclassProc(hwnd,message,w,l);}
 static HWND scroller(HWND tabs){for(auto child=GetWindow(tabs,GW_CHILD);child;child=GetWindow(child,GW_HWNDNEXT)){wchar_t name[64]{};GetClassNameW(child,name,64);if(IsWindowVisible(child)&&!_wcsicmp(name,UPDOWN_CLASSW))return child;}return nullptr;}
 static BOOL CALLBACK styleScroller(HWND hwnd,LPARAM pointer){wchar_t name[64]{};GetClassNameW(hwnd,name,64);if(!_wcsicmp(name,UPDOWN_CLASSW))SetWindowSubclass(hwnd,scrollerProcedure,2,DWORD_PTR(pointer));return TRUE;}
 static LRESULT CALLBACK tabsProcedure(HWND hwnd,UINT message,WPARAM w,LPARAM l,UINT_PTR,DWORD_PTR pointer){
  auto self=reinterpret_cast<Chrome*>(pointer);
  if(message==WM_ERASEBKGND)return TRUE;
  if(message==WM_MOUSEMOVE){TCHITTESTINFO hit{};hit.pt={short(LOWORD(l)),short(HIWORD(l))};int index=TabCtrl_HitTest(hwnd,&hit);if(index!=self->hovered){self->hovered=index;InvalidateRect(hwnd,nullptr,FALSE);}TRACKMOUSEEVENT track{sizeof(track),TME_LEAVE,hwnd,0};TrackMouseEvent(&track);}
  if(message==WM_MOUSELEAVE){self->hovered=-1;InvalidateRect(hwnd,nullptr,FALSE);}
  if(message==WM_SETFOCUS||message==WM_KILLFOCUS)InvalidateRect(hwnd,nullptr,FALSE);
  if(message==WM_PAINT){PAINTSTRUCT paint{};HDC target=BeginPaint(hwnd,&paint);RECT bounds{};GetClientRect(hwnd,&bounds);HDC dc=CreateCompatibleDC(target);auto bitmap=CreateCompatibleBitmap(target,std::max(1L,bounds.right),std::max(1L,bounds.bottom));auto oldBitmap=SelectObject(dc,bitmap);fill(dc,bounds,self->background);
   for(int i=0;i<TabCtrl_GetItemCount(hwnd);i++){RECT r{};TabCtrl_GetItemRect(hwnd,i,&r);r.top=self->px(2);r.bottom=bounds.bottom-self->px(2);InflateRect(&r,-self->px(2),0);bool active=i==TabCtrl_GetCurSel(hwnd);auto bg=active?self->selected:i==self->hovered?self->surface:self->background;
    if(active||i==self->hovered)self->rounded(dc,r,bg);if(active){RECT stripe{r.left+self->px(6),r.bottom-self->px(2),r.right-self->px(6),r.bottom};fill(dc,stripe,self->accent());}else{RECT divider{r.right,r.top+self->px(7),r.right+1,r.bottom-self->px(7)};fill(dc,divider,self->line);}
    wchar_t title[256]{};TCITEMW item{};item.mask=TCIF_TEXT;item.pszText=title;item.cchTextMax=256;TabCtrl_GetItem(hwnd,i,&item);InflateRect(&r,-self->px(10),0);if(active)r.right-=self->px(22);self->label(dc,r,title,readable(active?self->text:self->muted,bg),DT_LEFT);if(active&&GetFocus()==hwnd)DrawFocusRect(dc,&r);
   }
   BitBlt(target,0,0,bounds.right,bounds.bottom,dc,0,0,SRCCOPY);SelectObject(dc,oldBitmap);DeleteObject(bitmap);DeleteDC(dc);EndPaint(hwnd,&paint);return 0;}
  return DefSubclassProc(hwnd,message,w,l);
 }
 void layout(HWND tabs,int width){
  if(!tabs)return;int actual=TabCtrl_GetItemCount(tabs),count=std::max(1,actual),sel=TabCtrl_GetCurSel(tabs);int item=std::min(px(216),std::max(px(104),(width-px(8))/count));bool fontChanged=appliedDpi!=dpi,changed=fontChanged||lastWidth!=width||lastCount!=actual||lastSelected!=sel||lastItem!=item;
  // Commit cached metrics before native focus notifications can reenter resize.
  appliedDpi=dpi;lastWidth=width;lastCount=actual;lastSelected=sel;bool itemChanged=lastItem!=item;lastItem=item;
  if(fontChanged)SendMessageW(tabs,WM_SETFONT,reinterpret_cast<WPARAM>(font),FALSE);
  if(fontChanged||itemChanged)TabCtrl_SetItemSize(tabs,item,height()-px(6));
  // TCS_BUTTONS has an independent logical focus item. Reset the scroll anchor
  // before focusing the selected item; item-size/DPI changes otherwise leave a
  // formerly visible selection clipped, or retain an unnecessary scroll page.
  if(changed&&sel>=0){TabCtrl_SetCurFocus(tabs,sel?0:-1);TabCtrl_SetCurFocus(tabs,sel);}
  EnumChildWindows(tabs,styleScroller,reinterpret_cast<LPARAM>(this));InvalidateRect(tabs,nullptr,FALSE);
 }

 void install(HWND tabs){TabCtrl_SetPadding(tabs,0,0);SetWindowSubclass(tabs,tabsProcedure,1,reinterpret_cast<DWORD_PTR>(this));for(int id:{101,102})SetWindowSubclass(GetDlgItem(GetParent(tabs),id),buttonProcedure,1,reinterpret_cast<DWORD_PTR>(this));layout(tabs,1200);}
};
}
