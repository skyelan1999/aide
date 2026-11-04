param([Parameter(Mandatory=$true)][string]$TokenFile,[int]$Port=17778)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$native = @'
using System;
using System.Diagnostics;
using System.Drawing;
using System.Drawing.Imaging;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
public static class AideNativeComputer {
  [StructLayout(LayoutKind.Sequential)] public struct Rect { public int Left, Top, Right, Bottom; }
  [StructLayout(LayoutKind.Sequential)] struct Input { public uint Type; public InputUnion Data; }
  [StructLayout(LayoutKind.Explicit)] struct InputUnion { [FieldOffset(0)] public MouseInput Mouse; [FieldOffset(0)] public KeyInput Key; }
  [StructLayout(LayoutKind.Sequential)] struct MouseInput { public int X,Y; public uint MouseData,Flags,Time; public IntPtr Extra; }
  [StructLayout(LayoutKind.Sequential)] struct KeyInput { public ushort Vk,Scan; public uint Flags,Time; public IntPtr Extra; }
  [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h,out uint pid);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr h,StringBuilder s,int n);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h,out Rect r);
  [DllImport("user32.dll")] static extern bool IsIconic(IntPtr h);
  [DllImport("user32.dll")] static extern bool SetCursorPos(int x,int y);
  [DllImport("user32.dll",SetLastError=true)] static extern uint SendInput(uint n,Input[] inputs,int size);
  public static uint ProcessId(IntPtr h) { uint pid; GetWindowThreadProcessId(h,out pid); return pid; }
  public static string Title(IntPtr h) { var s=new StringBuilder(1024); GetWindowText(h,s,s.Capacity); return s.ToString(); }
  public static string ProcessName(IntPtr h) { uint pid=ProcessId(h); using(var p=Process.GetProcessById((int)pid)) return p.ProcessName; }
  public static byte[] Screenshot(IntPtr h,out Rect rect) {
    if(IsIconic(h)||!GetWindowRect(h,out rect)) throw new InvalidOperationException("Foreground app window is unavailable");
    int w=rect.Right-rect.Left, height=rect.Bottom-rect.Top;
    if(w<1||height<1||w>4096||height>4096||(long)w*height>4000000) throw new InvalidOperationException("App window exceeds screenshot limits");
    using(var bitmap=new Bitmap(w,height)) using(var graphics=Graphics.FromImage(bitmap)) using(var output=new MemoryStream()) {
      graphics.CopyFromScreen(rect.Left,rect.Top,0,0,bitmap.Size,CopyPixelOperation.SourceCopy);
      bitmap.Save(output,ImageFormat.Png); if(output.Length>2*1024*1024) throw new InvalidOperationException("Screenshot exceeds 2 MiB"); return output.ToArray();
    }
  }
  public static void Click(int x,int y) {
    if(!SetCursorPos(x,y)) throw new InvalidOperationException("Windows refused pointer movement");
    var inputs=new Input[2]; for(int i=0;i<2;i++) inputs[i].Type=0;
    inputs[0].Data.Mouse.Flags=0x0002; inputs[1].Data.Mouse.Flags=0x0004;
    if(SendInput(2,inputs,Marshal.SizeOf(typeof(Input)))!=2) throw new InvalidOperationException("Windows refused click input");
  }
  public static void TypeText(string text) {
    if(text.Length>2000) throw new InvalidOperationException("Input exceeds limit");
    foreach(char c in text) { var inputs=new Input[2]; for(int i=0;i<2;i++){inputs[i].Type=1;inputs[i].Data.Key.Scan=c;inputs[i].Data.Key.Flags=0x0004|(i==1?0x0002u:0u);} if(SendInput(2,inputs,Marshal.SizeOf(typeof(Input)))!=2) throw new InvalidOperationException("Windows refused text input"); }
  }
  public static void Key(ushort key) { var inputs=new Input[2];for(int i=0;i<2;i++){inputs[i].Type=1;inputs[i].Data.Key.Vk=key;inputs[i].Data.Key.Flags=i==1?0x0002u:0u;}if(SendInput(2,inputs,Marshal.SizeOf(typeof(Input)))!=2)throw new InvalidOperationException("Windows refused key input"); }
}
'@
Add-Type -TypeDefinition $native -ReferencedAssemblies System.Drawing

function Read-Token {
  if (-not (Test-Path -LiteralPath $TokenFile)) { return '' }
  return (Get-Content -LiteralPath $TokenFile -Raw).Trim()
}
function Send-Response($stream,[int]$status,$value) {
  $body=[Text.Encoding]::UTF8.GetBytes(($value | ConvertTo-Json -Depth 8 -Compress))
  $reason=if($status -eq 200){'OK'}elseif($status -eq 401){'Unauthorized'}elseif($status -eq 404){'Not Found'}else{'Bad Request'}
  $header=[Text.Encoding]::ASCII.GetBytes("HTTP/1.1 $status $reason`r`nContent-Type: application/json; charset=utf-8`r`nCache-Control: no-store`r`nConnection: close`r`nContent-Length: $($body.Length)`r`n`r`n")
  $stream.Write($header,0,$header.Length);$stream.Write($body,0,$body.Length);$stream.Flush()
}
function Read-Request($stream) {
  $header=New-Object System.Collections.Generic.List[byte]
  while($header.Count -lt 16384) {
    $b=$stream.ReadByte();if($b -lt 0){throw 'Incomplete HTTP request'};$header.Add([byte]$b)
    $n=$header.Count;if($n -ge 4 -and $header[$n-4] -eq 13 -and $header[$n-3] -eq 10 -and $header[$n-2] -eq 13 -and $header[$n-1] -eq 10){break}
  }
  if($header.Count -ge 16384){throw 'Request headers too large'}
  $text=[Text.Encoding]::ASCII.GetString($header.ToArray());$lines=$text -split "`r`n";$first=$lines[0] -split ' '
  if($first.Count -ne 3){throw 'Invalid HTTP request line'}
  $headers=@{};foreach($line in $lines | Select-Object -Skip 1){$at=$line.IndexOf(':');if($at -gt 0){$headers[$line.Substring(0,$at).Trim().ToLowerInvariant()]=$line.Substring($at+1).Trim()}}
  if($headers.ContainsKey('transfer-encoding')){throw 'Transfer encoding is unsupported'}
  $length=0;if($headers.ContainsKey('content-length')){if(-not [int]::TryParse($headers['content-length'],[ref]$length)){throw 'Invalid content length'}}
  if($length -lt 0 -or $length -gt 16384){throw 'Request body too large'}
  $body=New-Object byte[] $length;$offset=0;while($offset -lt $length){$read=$stream.Read($body,$offset,$length-$offset);if($read -le 0){throw 'Incomplete HTTP body'};$offset+=$read}
  return @{Method=$first[0].ToUpperInvariant();Path=($first[1] -split '\?')[0];Headers=$headers;Body=$body}
}
function Assert-AllowedTarget($payload) {
  $apps=$payload.allowedApps
  if($null -eq $apps -or $apps.Count -lt 1 -or $apps.Count -gt 50){throw 'Allowed app list is invalid'}
  $window=[AideNativeComputer]::GetForegroundWindow();$processId=[AideNativeComputer]::ProcessId($window)
  if($window -eq [IntPtr]::Zero -or $processId -eq 0){throw 'No foreground app'}
  $name=[AideNativeComputer]::ProcessName($window);$title=[AideNativeComputer]::Title($window)
  $allowed=$false;foreach($app in $apps){if($app -is [string] -and ($app -ieq $name -or $app -ieq ($name+'.exe') -or ($title -and $app -ceq $title))){$allowed=$true}}
  if(-not $allowed){throw 'Foreground app is not in Aide allowed-app list'}
  return @{Window=$window;Pid=$processId;Name=$name;Title=$title}
}

$listener=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,$Port);$listener.Start()
try {
  while($true) {
    $client=$listener.AcceptTcpClient();$client.ReceiveTimeout=5000;$client.SendTimeout=5000;$stream=$client.GetStream()
    try {
      $request=Read-Request $stream;$token=Read-Token
      if($token.Length -lt 32 -or $request.Headers['authorization'] -cne "Bearer $token"){Send-Response $stream 401 @{ok=$false;error='Unauthorized'};continue}
      if($request.Method -eq 'GET' -and $request.Path -eq '/healthz'){Send-Response $stream 200 @{ok=$true;service='aide-windows-computer-bridge';platform='windows'};continue}
      if($request.Method -ne 'POST'){Send-Response $stream 404 @{ok=$false;error='Unsupported operation'};continue}
      $payload=[Text.Encoding]::UTF8.GetString($request.Body) | ConvertFrom-Json
      if($request.Path -eq '/v1/inspect'){Send-Response $stream 200 @{ok=$true;platform='windows';controls=@();warning='Windows native inspection is not available; use screenshots.'};continue}
      $target=Assert-AllowedTarget $payload
      if($request.Path -eq '/v1/snapshot'){
        $rect=New-Object -TypeName 'AideNativeComputer+Rect';$png=[AideNativeComputer]::Screenshot($target.Window,[ref]$rect)
        if([AideNativeComputer]::ProcessId([AideNativeComputer]::GetForegroundWindow()) -ne $target.Pid){throw 'Foreground app changed; screenshot discarded'}
        Send-Response $stream 200 @{ok=$true;app=$target.Name;mimeType='image/png';imageBase64=[Convert]::ToBase64String($png);window=@{x=$rect.Left;y=$rect.Top;width=($rect.Right-$rect.Left);height=($rect.Bottom-$rect.Top)}};continue
      }
      if([AideNativeComputer]::ProcessId([AideNativeComputer]::GetForegroundWindow()) -ne $target.Pid -or $payload.approved -ne $true){throw 'Aide confirmation missing or foreground app changed'}
      if($request.Path -eq '/v1/click'){
        $x=[int]$payload.x;$y=[int]$payload.y;$rect=New-Object -TypeName 'AideNativeComputer+Rect'
        if(-not [AideNativeComputer]::GetWindowRect($target.Window,[ref]$rect) -or $x -lt $rect.Left -or $x -ge $rect.Right -or $y -lt $rect.Top -or $y -ge $rect.Bottom){throw 'Click coordinate is outside the allowed app window'}
        [AideNativeComputer]::Click($x,$y);Send-Response $stream 200 @{ok=$true;app=$target.Name;executed=$true};continue
      }
      if($request.Path -eq '/v1/type'){[AideNativeComputer]::TypeText([string]$payload.text);Send-Response $stream 200 @{ok=$true;app=$target.Name;executed=$true};continue}
      if($request.Path -eq '/v1/key'){
        $keys=@{ENTER=13;TAB=9;ESCAPE=27;BACKSPACE=8;DELETE=46;UP=38;DOWN=40;LEFT=37;RIGHT=39;SPACE=32};$key=([string]$payload.key).ToUpperInvariant()
        if(-not $keys.ContainsKey($key)){throw 'Unsupported key'};[AideNativeComputer]::Key([uint16]$keys[$key]);Send-Response $stream 200 @{ok=$true;app=$target.Name;executed=$true};continue
      }
      Send-Response $stream 404 @{ok=$false;error='Unsupported operation'}
    } catch { try{Send-Response $stream 404 @{ok=$false;error=$_.Exception.Message}}catch{} }
    finally {$stream.Dispose();$client.Dispose()}
  }
} finally {$listener.Stop()}
