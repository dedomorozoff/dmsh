; installer-bundle.iss - Inno Setup script for offline installer with bundled model
; Run: iscc installer-bundle.iss

[Setup]
AppName=dmsh
AppVersion=1.0.0
DefaultDirName={userappdata}\Programs\dmsh
DefaultGroupName=dmsh
UninstallDisplayIcon={app}\dmsh.exe
OutputDir=.
OutputBaseFilename=dmsh-setup-bundle
Compression=lzma2/ultra64
SolidCompression=yes
ChangesEnvironment=yes
PrivilegesRequired=lowest
ExtraDiskSpaceRequired=700000000

[Files]
; Main executable
Source: "bin\dmsh.exe"; DestDir: "{app}"; Flags: ignoreversion
; Bundled model (will be moved to user config dir during install)
Source: "bundle\qwen2.5-0.5b-instruct-q4_k_m.gguf"; DestDir: "{app}"; Flags: ignoreversion
; MinGW runtime DLLs
Source: "bundle\libstdc++-6.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "bundle\libgcc_s_seh-1.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "bundle\libgomp-1.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "bundle\libwinpthread-1.dll"; DestDir: "{app}"; Flags: ignoreversion

[Tasks]
Name: envPath; Description: "Add dmsh to user PATH"; Flags: checkedonce

[Registry]
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Tasks: envPath; Check: NotOnPathYet

[Run]
; Configure bundled model as default
Filename: "{app}\dmsh.exe"; Parameters: "model use qwen2.5-0.5b-instruct-q4_k_m"; Description: "Configure bundled model as default"; Flags: postinstall waituntilterminated runhidden

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
    ModelSource := ExpandConstant('{app}\qwen2.5-0.5b-instruct-q4_k_m.gguf');
    ConfigDir := ExpandConstant('{userappdata}') + '\dmsh\models';
    ModelDest := ConfigDir + '\qwen2.5-0.5b-instruct-q4_k_m.gguf';

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
