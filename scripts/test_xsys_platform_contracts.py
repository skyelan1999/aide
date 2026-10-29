#!/usr/bin/env python3
"""Run actual vendored function bodies with deterministic OS-call fixtures.

These are boundary/return-value contracts, not Windows/BSD hardware acceptance.
Linux public-API tests live in scripts/xsys-review and run without extraction.
"""
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SYS = Path(os.environ.get('AIDE_XSYS_ROOT', ROOT / 'vendor/golang.org/x/sys'))
GO = os.environ.get('AIDE_TEST_GO') or shutil.which('go')


def function(file, signature):
    text = (SYS / file).read_text()
    start = text.index('func ' + signature)
    following = re.search(r'\nfunc ', text[start + 1:])
    section = text[start:start + 1 + following.start()] if following else text[start:]
    return section[:section.rindex('\n}') + 2] + '\n'


class Contracts(unittest.TestCase):
    def run_go(self, source, imports, setup, test):
        with tempfile.TemporaryDirectory(prefix='aide-xsys-contract-') as name:
            p = Path(name)
            (p/'go.mod').write_text('module contract\n\ngo 1.26\n')
            (p/'contract_test.go').write_text('package contract\nimport (' + ';'.join('"'+s+'"' for s in ['testing']+imports) + ')\n' + setup + '\n' + source + '\nfunc TestContract(t *testing.T) {\n' + test + '\n}\n')
            result = subprocess.run([GO, 'test', '-count=1', '-timeout=30s', '-v', '.'], cwd=p, env={**os.environ, 'GOWORK':'off', 'GOMAXPROCS':'2'}, text=True, capture_output=True, timeout=90)
            self.assertEqual(result.returncode, 0, result.stdout+result.stderr)

    def test_292_cpu_flags_are_independent(self):
        source = function('cpu/cpu_windows_arm64.go', 'doinit()')
        flags = re.findall(r'_PF_[A-Z0-9_]+', source)
        names = sorted(set(re.findall(r'ARM64\.(\w+)', source)))
        self.run_go(source, [], 'const ('+'\n'.join(s+' = '+str(i) for i,s in enumerate(dict.fromkeys(flags)))+')\nvar ARM64 struct {'+';'.join(s+' bool' for s in names+(['HasSM3'] if 'HasSM3' not in names else []))+'}\nfunc isProcessorFeaturePresent(n int) bool { return n == _PF_ARM_V83_LRCPC_INSTRUCTIONS_AVAILABLE }', 'doinit(); if !ARM64.HasLRCPC || ARM64.HasSM3 || ARM64.HasAES {t.Fatalf("flags: %+v",ARM64)}')

    def test_293_netbsd_missing_cpu_node(self):
        source = function('cpu/cpu_netbsd_arm64.go', 'nametomib(')
        self.run_go(source, ['syscall'], '''var _ = syscall.ENOENT
type sysctlNode struct { Num int32; Name [32]int8 }
var fixture []sysctlNode
func sysctlNodes([]int32) ([]sysctlNode,error) {return fixture,nil}''', '''if mib,err:=nametomib("absent");err==nil||mib!=nil {t.Fatalf("missing: %v %v",mib,err)}
fixture=[]sysctlNode{{Num:7,Name:[32]int8{'c','p','u'}}};mib,err:=nametomib("cpu");if err!=nil||len(mib)!=1||mib[0]!=7 {t.Fatalf("valid: %v %v",mib,err)}''')

    def test_293_300_netbsd_empty_node_queries(self):
        for file, node, integer, ctl, vers in [('cpu/cpu_netbsd_arm64.go','sysctlNode','int32','_CTL_QUERY','_SYSCTL_VERS_1'), ('unix/syscall_netbsd.go','Sysctlnode','_C_int','CTL_QUERY','SYSCTL_VERS_1')]:
            with self.subTest(file=file):
                source=function(file,'sysctlNodes(')
                self.run_go(source,['syscall','unsafe'],f'''var _ = syscall.ENOENT
const ENOENT=syscall.ENOENT
type _C_int=int32
type {node} struct {{ Flags uint32; Num int32 }}
const {ctl}=-2
const {vers}=0x1000000
var count uintptr
func sysctl(_ []{integer}, output *byte, n *uintptr, _ *byte, _ uintptr) error {{ *n=count*unsafe.Sizeof({node}{{}}); if output!=nil {{(*{node})(unsafe.Pointer(output)).Num=7}};return nil }}''','''if nodes,err:=sysctlNodes(nil);err==nil||nodes!=nil {t.Fatalf("empty: %v %v",nodes,err)}
count=1;nodes,err:=sysctlNodes(nil);if err!=nil||len(nodes)!=1||nodes[0].Num!=7 {t.Fatalf("valid: %v %v",nodes,err)}''')

    def test_296_tee_ignores_second_register(self):
        generated=(SYS/'unix/zsyscall_linux_386.go').read_text()
        source=(function('unix/zsyscall_linux_386.go','tee(')+function('unix/syscall_linux.go','Tee(')
                if 'func tee(' in generated else function('unix/zsyscall_linux_386.go','Tee('))
        self.run_go(source,[],'''const SYS_TEE=1
type errno int
func(e errno)Error()string{return "errno"}
func errnoErr(e errno)error{return e}
func Syscall6(uintptr,uintptr,uintptr,uintptr,uintptr,uintptr,uintptr)(uintptr,uintptr,errno){return 5,5,0}''','n,err:=Tee(1,2,5,0);if err!=nil||n!=5{t.Fatalf("result: %d %v",n,err)}')

    def test_297_freebsd_reserved_capability(self):
        text=(SYS/'unix/cap_freebsd.go').read_text();source=text[text.index('const ('):text.index('// CapRightsLimit')]
        self.run_go(source,['errors','fmt'],'const CAP_RIGHTS_VERSION_00=0\ntype CapRights struct {Rights [2]uint64}', '''bad:=uint64(3)<<57|1
if _,err:=CapRightsInit([]uint64{bad});err==nil{t.Fatal("reserved init accepted")}
r,err:=CapRightsInit([]uint64{uint64(1)<<57|1});if err!=nil{t.Fatal(err)}
if err:=CapRightsSet(r,[]uint64{bad});err==nil{t.Fatal("reserved set accepted")}
if err:=CapRightsClear(r,[]uint64{bad});err==nil{t.Fatal("reserved clear accepted")}
if _,err:=CapRightsIsSet(r,[]uint64{bad});err==nil{t.Fatal("reserved query accepted")}
if ok,err:=CapRightsIsSet(r,[]uint64{uint64(1)<<57|1});err!=nil||!ok{t.Fatal("valid right lost")}
if err:=CapRightsClear(r,[]uint64{uint64(1)<<57|1});err!=nil{t.Fatal(err)}''')

    def test_298_generator_checkout_with_spaces(self):
        with tempfile.TemporaryDirectory(prefix='aide xsys ') as name:
            p=Path(name);(p/'unix').mkdir();shutil.copyfile(SYS/'unix/mkall.sh',p/'unix/mkall.sh')
            capture=p/'docker-argv.json'
            (p/'docker').write_text('#!/usr/bin/env python3\nimport json,os,sys\nif "--volume" in sys.argv:\n json.dump(sys.argv,open(os.environ["AIDE_XSYS_ARGV"],"w"))\n')
            (p/'docker').chmod(0o700)
            subprocess.run(['bash',str(p/'unix/mkall.sh')],cwd=p/'unix',env={**os.environ,'GOOS':'linux','GOARCH':'amd64','PATH':str(p)+os.pathsep+os.environ['PATH'],'AIDE_XSYS_ARGV':str(capture)},check=True,timeout=10)
            import json
            args=json.loads(capture.read_text());self.assertEqual(args[args.index('--volume')+1],str(p)+':/build');self.assertEqual(args[-1],'generate:linux')

    def test_301_bsd_xattr_error_propagation(self):
        source='\n'.join(function('unix/xattr_bsd.go',n) for n in ['initxattrdest(', 'ListxattrNS(', 'FlistxattrNS(', 'LlistxattrNS('])
        self.run_go(source,['unsafe','syscall'],'''var _zero byte
var failure error=syscall.EPERM
func ExtattrListFile(string,int,uintptr,int)(int,error){return 7,failure}
func ExtattrListFd(int,int,uintptr,int)(int,error){return 7,failure}
func ExtattrListLink(string,int,uintptr,int)(int,error){return 7,failure}''','''for _,f:=range []func()(int,error){func()(int,error){return ListxattrNS("x",1,nil)},func()(int,error){return FlistxattrNS(3,1,nil)},func()(int,error){return LlistxattrNS("x",1,nil)}} {
if _,err:=f();err!=syscall.EPERM{t.Fatalf("lost failure: %v",err)};failure=nil;if n,err:=f();n!=7||err!=nil{t.Fatalf("valid: %d %v",n,err)};failure=syscall.EPERM}''')

    def test_302_windows_descriptor_error(self):
        source=function('windows/security_windows.go','(sd *SECURITY_DESCRIPTOR) DACL(')+function('windows/security_windows.go','(sd *SECURITY_DESCRIPTOR) SACL(')
        self.run_go(source,['syscall'],'''type ACL struct{}
type SECURITY_DESCRIPTOR struct{}
var failure error=syscall.EINVAL
var presentValue bool
const ERROR_OBJECT_NOT_FOUND=syscall.ENOENT
func getSecurityDescriptorDacl(_ *SECURITY_DESCRIPTOR,present *bool,_ **ACL,_ *bool)error{*present=presentValue;return failure}
func getSecurityDescriptorSacl(_ *SECURITY_DESCRIPTOR,present *bool,_ **ACL,_ *bool)error{*present=presentValue;return failure}''','''sd:=&SECURITY_DESCRIPTOR{}
for _,f:=range []func()(*ACL,bool,error){sd.DACL,sd.SACL}{failure=syscall.EINVAL;presentValue=false;if _,_,err:=f();err!=failure{t.Fatalf("lost error: %v",err)};failure=nil;if _,_,err:=f();err!=ERROR_OBJECT_NOT_FOUND{t.Fatalf("absent: %v",err)};presentValue=true;if _,_,err:=f();err!=nil{t.Fatal(err)}}''')

    def test_303_windows_working_set_bits(self):
        source='\n'.join(function('windows/syscall_windows.go','(b PSAPI_WORKING_SET_EX_BLOCK) '+n+'()') for n in ['Shared','Locked','LargePage','Bad'])
        self.run_go(source,[],'type PSAPI_WORKING_SET_EX_BLOCK uint64','''for bit,read:=range map[uint]func(PSAPI_WORKING_SET_EX_BLOCK)bool{15:PSAPI_WORKING_SET_EX_BLOCK.Shared,22:PSAPI_WORKING_SET_EX_BLOCK.Locked,23:PSAPI_WORKING_SET_EX_BLOCK.LargePage,31:PSAPI_WORKING_SET_EX_BLOCK.Bad}{if read(0)||read(1)||!read(1<<bit){t.Fatalf("bit %d",bit)}}''')

    def test_304_temporary_sockets_close_once(self):
        source='\n'.join(function('windows/syscall_windows.go',n) for n in ['LoadConnectEx()', 'loadWSASendRecvMsg()'])
        self.run_go(source,['sync','unsafe','errors'],'''type Handle uintptr
type Overlapped struct{}
const(AF_INET=2;SOCK_STREAM=1;SOCK_DGRAM=2;IPPROTO_TCP=6;IPPROTO_UDP=17;SIO_GET_EXTENSION_FUNCTION_POINTER=1)
var WSAID_CONNECTEX,WSAID_WSARECVMSG,WSAID_WSASENDMSG [16]byte
var connectExFunc struct {once sync.Once;addr uintptr;err error}
var sendRecvMsgFunc struct {once sync.Once;sendAddr,recvAddr uintptr;err error}
var opened,closed,wrong int
var failure error
func Socket(int,int,int)(Handle,error){opened++;return 7,nil}
func Closesocket(Handle)error{closed++;return nil}
func CloseHandle(Handle)error{wrong++;return nil}
func WSAIoctl(Handle,uint32,*byte,uint32,*byte,uint32,*uint32,*Overlapped,uintptr)error{return failure}''','''for _,bad:=range []bool{false,true}{connectExFunc.once=sync.Once{};sendRecvMsgFunc.once=sync.Once{};failure=nil;if bad{failure=errors.New("ioctl failed")};LoadConnectEx();LoadConnectEx();loadWSASendRecvMsg();loadWSASendRecvMsg()};if opened!=4||closed!=4||wrong!=0{t.Fatalf("open/close/wrong: %d/%d/%d",opened,closed,wrong)}''')

    def test_305_success_bool_partial_privilege(self):
        source=function('windows/zsyscall_windows.go','AdjustTokenPrivileges(').replace('syscall.SyscallN','fixtureCall')
        self.run_go(source,['unsafe','syscall'],'''type Token uintptr
type Tokenprivileges struct{}
type procedure struct{}
func(procedure)Addr()uintptr{return 1}
var procAdjustTokenPrivileges procedure
var retval uintptr=1
var lastError syscall.Errno=1300
const ERROR_NOT_ALL_ASSIGNED=syscall.Errno(1300)
func errnoErr(e syscall.Errno)error{return e}
func fixtureCall(...uintptr)(uintptr,uintptr,syscall.Errno){return retval,0,lastError}''','''if err:=AdjustTokenPrivileges(1,false,nil,0,nil,nil);err!=ERROR_NOT_ALL_ASSIGNED{t.Fatalf("partial: %v",err)};lastError=0;if err:=AdjustTokenPrivileges(1,false,nil,0,nil,nil);err!=nil{t.Fatalf("success: %v",err)};retval=0;lastError=syscall.EINVAL;if err:=AdjustTokenPrivileges(1,false,nil,0,nil,nil);err!=syscall.EINVAL{t.Fatal(err)}''')

if __name__=='__main__': unittest.main(verbosity=2)
