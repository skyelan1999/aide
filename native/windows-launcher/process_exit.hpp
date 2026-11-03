#pragma once
// A failed termination call can race with natural exit. Only retained process
// handles plus a successfully queried empty job establish the stopped state.
inline bool nativeJobStopped(HANDLE job,const std::vector<HANDLE>& processes,DWORD& error){
 bool exited=true;for(auto process:processes){DWORD state=WaitForSingleObject(process,0);if(state==WAIT_FAILED){error=GetLastError();return false;}if(state!=WAIT_OBJECT_0)exited=false;}
 JOBOBJECT_BASIC_ACCOUNTING_INFORMATION info{};if(!QueryInformationJobObject(job,JobObjectBasicAccountingInformation,&info,sizeof(info),nullptr)){error=GetLastError();return false;}
 return exited&&!info.ActiveProcesses;
}
inline void terminateAndConfirm(HANDLE job,const std::vector<HANDLE>& processes,DWORD code,DWORD timeout=5000){
 DWORD observationError=0;if(nativeJobStopped(job,processes,observationError))return;
 BOOL requested=TerminateJobObject(job,code);DWORD terminateError=requested?0:GetLastError();auto until=GetTickCount64()+timeout;
 do{observationError=0;if(nativeJobStopped(job,processes,observationError))return;if(GetTickCount64()>=until)break;Sleep(25);}while(true);
 throw aide_host::Unsafe("Native process tree termination unconfirmed (Windows error "+std::to_string(terminateError)+", observation error "+std::to_string(observationError)+"); stop required");
}
