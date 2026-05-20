; installer-bundle.iss - Inno Setup script for offline installer with bundled model
; Run: iscc installer-bundle.iss

[Setup]
AppName=nlsh
AppVersion=1.0.0
DefaultDirName={userappdata}\Programs\nlsh
DefaultGroupName=nlsh
UninstallDisplayIcon={app}\nlsh.exe
OutputDir=.
OutputBaseFilename=nlsh-setup-bundle
Compression=lzma2/ultra64
SolidCompression=yes
ChangesEnvironment=yes
PrivilegesRequired=lowest
ExtraDiskSpaceRequired=4500000000

[Files]
; Main executable
Source: "bin\nlsh.exe"; DestDir: "{app}"; Flags: ignoreversion
; Bundled model (will be moved to user config dir during install)
Source: "bundle\Qwopus3.5-9B-coder-Exp-Q3_K_S.gguf"; DestDir: "{app}"; Flags: ignoreversion
; MinGW runtime DLLs
Source: "bundle\libstdc++-6.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "bundle\libgcc_s_seh-1.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "bundle\libgomp-1.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "bundle\libwinpthread-1.dll"; DestDir: "{app}"; Flags: ignoreversion

[Tasks]
Name: envPath; Description: "Add nlsh to user PATH"; Flags: checkedonce

[Registry]
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Tasks: envPath; Check: NotOnPathYet

[Run]
; Configure bundled model as default
Filename: "{app}\nlsh.exe"; Parameters: "model use Qwopus3.5-9B-coder-Exp-Q3_K_S"; Description: "Configure bundled model as default"; Flags: postinstall waituntilterminated runhidden

[Code]
function NotOnPathYet(): Boolean;
var
  PathStr: string;
begin
  if RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', PathStr) then
  begin
    Result := Pos(ExpandConstant('{app}'), PathStr) = 0;
  end
  else
  begin
    Result := True;
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  ModelSource, ModelDest, ConfigDir: string;
begin
  if CurStep = ssPostInstall then
  begin
    // Move bundled model to user's config directory
    ModelSource := ExpandConstant('{app}\Qwopus3.5-9B-coder-Exp-Q3_K_S.gguf');
    ConfigDir := ExpandConstant('{userappdata}') + '\nlsh\models';
    ModelDest := ConfigDir + '\Qwopus3.5-9B-coder-Exp-Q3_K_S.gguf';

    if not DirExists(ConfigDir) then
      ForceDirectories(ConfigDir);

    // Copy model file to user config (keep original in app dir for now)
    if FileExists(ModelSource) then
    begin
      if not FileExists(ModelDest) then
      begin
        FileCopy(ModelSource, ModelDest, False);
      end;
      // Remove from app directory to save disk space
      DeleteFile(ModelSource);
    end;
  end;
end;
