#include "../native/windows-launcher/desktop_policy.hpp"
#include <cassert>
#include <iostream>
int main(){using namespace aide_desktop;std::wstring origin=L"https://localhost:9999";
 for(auto url:{L"https://localhost:9999/",L"https://localhost:9999/#pluginWorkbench=compare&workspace=local%7CC%3A%5Cdata%7C",L"blob:https://localhost:9999/id"})assert(local(url,origin));
 for(auto url:{L"https://localhost:9998/",L"https://localhost:9999.evil/",L"https://localhost.evil:9999/",L"https://user@localhost:9999/",L"http://localhost:9999/",L"file:///C:/secret",L"javascript:alert(1)",L"blob:blob:https://localhost:9999/id",L"https://localhost:9999/\n",L"https://localhost:9999/\\evil"})assert(!local(url,origin));
 assert(authRoot(origin+L"/",origin));for(auto path:{L"/#pluginWorkbench=compare",L"/?external=1",L"/plugin-workbench/owned",L"/#token=fixture",L"/#%70luginWorkbench=compare"})assert(!authRoot(origin+path,origin));
 assert(external(L"https://learn.microsoft.com/"));assert(external(L"mailto:example@example.test"));assert(!external(L"javascript:alert(1)"));assert(!external(L"file:///C:/secret"));assert(!external(L"https://user:secret@example.test/"));
 std::cout<<"PASS desktop same-origin, native-auth frame URL, external-link guards; no Win32 execution claimed\n";
}
