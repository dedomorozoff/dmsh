; installer.iss - Скрипт Inno Setup для создания GUI-установщика nlsh под Windows

[Setup]
AppName=nlsh
AppVersion=1.0.0
DefaultDirName={userappdata}\Programs\nlsh
DefaultGroupName=nlsh
UninstallDisplayIcon={app}\nlsh.exe
OutputDir=.
OutputBaseFilename=nlsh-setup-online
Compression=lzma
SolidCompression=yes
ChangesEnvironment=yes
PrivilegesRequired=lowest

[Files]
Source: "bin\nlsh.exe"; DestDir: "{app}"; Flags: ignoreversion

[Tasks]
Name: envPath; Description: "Добавить nlsh в переменную PATH пользователя"; Flags: checkedonce

[Registry]
; Добавление директории установки в PATH пользователя
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Tasks: envPath; Check: NotOnPathYet

[Run]
; Скачивание рекомендуемой модели в конце установки
Filename: "{app}\nlsh.exe"; Parameters: "model download --set-default"; Description: "Скачать рекомендуемую LLM-модель (автоматически выберет оптимальную под ОЗУ)"; Flags: postinstall waituntilterminated

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
