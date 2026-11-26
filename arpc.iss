#ifndef AppVer
  #define AppVer "1.0.0"
#endif
#ifndef StageExe
  #define StageExe "bin\ARPC.exe"
#endif

[Setup]
AppId={{6F1B7C52-3A4E-4D58-9B0E-ARPC00000001}
AppName=ARPC
AppVersion={#AppVer}
VersionInfoVersion={#AppVer}.0
AppPublisher=ARPC
DefaultDirName={autopf}\ARPC
DefaultGroupName=ARPC
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputDir=bin
OutputBaseFilename=ARPC-Setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
SetupIconFile=internal\webui\favicon.ico
UninstallDisplayIcon={app}\ARPC.exe
AppMutex=ARPC_SingleInstance
CloseApplications=yes
ArchitecturesInstallIn64BitMode=x64compatible

[Files]
Source: "{#StageExe}"; DestDir: "{app}"; DestName: "ARPC.exe"; Flags: ignoreversion
Source: "bin\ARPC.apk"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist

[Icons]
Name: "{group}\ARPC"; Filename: "{app}\ARPC.exe"; IconFilename: "{app}\ARPC.exe"
Name: "{group}\Uninstall ARPC"; Filename: "{uninstallexe}"

[Run]
Filename: "{app}\ARPC.exe"; Description: "Launch ARPC"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{sys}\taskkill.exe"; Parameters: "/F /IM ARPC.exe"; Flags: runhidden; RunOnceId: "KillARPC"
Filename: "{app}\ARPC.exe"; Parameters: "--remove-autostart"; Flags: runhidden; RunOnceId: "RemoveAutostart"
