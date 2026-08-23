; installer.iss - Скрипт Inno Setup для создания GUI-установщика dmsh под Windows

[Setup]
AppName=dmsh
AppVersion=1.0.0
DefaultDirName={userappdata}\Programs\dmsh
DefaultGroupName=dmsh
UninstallDisplayIcon={app}\dmsh.exe
OutputDir=.
OutputBaseFilename=dmsh-setup-online
Compression=lzma
SolidCompression=yes
ChangesEnvironment=yes
PrivilegesRequired=lowest

[Files]
Source: "bin\dmsh.exe"; DestDir: "{app}"; Flags: ignoreversion

[Tasks]
Name: envPath; Description: "Добавить dmsh в переменную PATH пользователя"; Flags: checkedonce

[Registry]
; Добавление директории установки в PATH пользователя
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Tasks: envPath; Check: NotOnPathYet

[Run]
; Скачивание рекомендуемой модели в конце установки
Filename: "{app}\dmsh.exe"; Parameters: "model download --set-default"; Description: "Скачать рекомендуемую LLM-модель (автоматически выберет оптимальную под ОЗУ)"; Flags: postinstall waituntilterminated

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
