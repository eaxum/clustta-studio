; Clustta Studio - Inno Setup Installer Script
; Collects configuration values during install and writes studio_config.json

#define MyAppName "Clustta Studio"
#define MyAppPublisher "Eaxum"
#define MyAppURL "https://clustta.com"
#define MyAppExeName "clustta-studio-server.exe"

; Version is passed via /DMyAppVersion=x.x.x on the ISCC command line.
; Falls back to "0.0.0" for local testing.
#ifndef MyAppVersion
  #define MyAppVersion "0.0.0"
#endif

[Setup]
AppId={{B8F2D4A1-3C7E-4F5A-9D1B-6E8A0C2F4D7E}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
DefaultDirName={autopf}\{#MyAppPublisher}\{#MyAppName}
DefaultGroupName={#MyAppName}
OutputDir=..\..\bin
OutputBaseFilename=clustta-studio-installer
Compression=lzma2
SolidCompression=yes
SetupIconFile=icon.ico
UninstallDisplayIcon={app}\icon.ico
ArchitecturesInstallIn64BitMode=x64compatible
WizardStyle=modern
WizardSizePercent=100
WizardResizable=no
LicenseFile=..\..\LICENSE
PrivilegesRequired=admin
MinVersion=10.0
UninstallDisplayName=Uninstall {#MyAppName}
SignTool=signtool

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "..\..\tmp\clustta-studio-server.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "icon.ico"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Parameters: "studio"; IconFilename: "{app}\icon.ico"
Name: "{group}\Uninstall {#MyAppName}"; Filename: "{uninstallexe}"

[UninstallDelete]
Type: files; Name: "{app}\studio_config.json"
Type: files; Name: "{app}\studio_server.log"

[Code]
var
  ConfigPage: TWizardPage;
  PortEdit: TNewEdit;
  ProjectsDirEdit: TNewEdit;
  ProjectsDirBrowse: TNewButton;
  ServerURLEdit: TNewEdit;
  ServerNameEdit: TNewEdit;
  APIKeyEdit: TNewEdit;
  PrivateCheckbox: TNewCheckBox;
  ExistingConfigFile: String;

procedure BrowseProjectsDir(Sender: TObject);
var
  Dir: String;
begin
  Dir := ProjectsDirEdit.Text;
  if BrowseForFolder('Select Projects Directory', Dir, False) then
    ProjectsDirEdit.Text := Dir;
end;

{ Simple JSON value extractor - finds "key": "value" or "key": true/false }
function GetJsonValue(const Json, Key: String): String;
var
  KeyPattern, ValueStart: String;
  StartPos, EndPos, I: Integer;
begin
  Result := '';
  KeyPattern := '"' + Key + '":';
  StartPos := Pos(KeyPattern, Json);
  if StartPos = 0 then Exit;
  
  StartPos := StartPos + Length(KeyPattern);
  
  { Skip whitespace }
  while (StartPos <= Length(Json)) and ((Json[StartPos] = ' ') or (Json[StartPos] = #9)) do
    StartPos := StartPos + 1;
  
  if StartPos > Length(Json) then Exit;
  
  { Check if it's a string value (starts with quote) or boolean }
  if Json[StartPos] = '"' then
  begin
    StartPos := StartPos + 1;
    EndPos := StartPos;
    while (EndPos <= Length(Json)) and (Json[EndPos] <> '"') do
    begin
      { Handle escaped characters }
      if (Json[EndPos] = '\') and (EndPos < Length(Json)) then
        EndPos := EndPos + 2
      else
        EndPos := EndPos + 1;
    end;
    Result := Copy(Json, StartPos, EndPos - StartPos);
    { Unescape backslashes }
    StringChangeEx(Result, '\\', '\', True);
  end
  else
  begin
    { Boolean or number }
    EndPos := StartPos;
    while (EndPos <= Length(Json)) and (Json[EndPos] <> ',') and (Json[EndPos] <> '}') and (Json[EndPos] <> #13) and (Json[EndPos] <> #10) do
      EndPos := EndPos + 1;
    Result := Trim(Copy(Json, StartPos, EndPos - StartPos));
  end;
end;

procedure LoadExistingConfig;
var
  ConfigContent: AnsiString;
  Value: String;
begin
  ExistingConfigFile := ExpandConstant('{autopf}\{#MyAppPublisher}\{#MyAppName}\studio_config.json');
  
  if FileExists(ExistingConfigFile) then
  begin
    if LoadStringFromFile(ExistingConfigFile, ConfigContent) then
    begin
      Value := GetJsonValue(ConfigContent, 'port');
      if Value <> '' then PortEdit.Text := Value;
      
      Value := GetJsonValue(ConfigContent, 'projects_dir');
      if Value <> '' then ProjectsDirEdit.Text := Value;
      
      Value := GetJsonValue(ConfigContent, 'server_url');
      if Value <> '' then ServerURLEdit.Text := Value;
      
      Value := GetJsonValue(ConfigContent, 'server_name');
      if Value <> '' then ServerNameEdit.Text := Value;
      
      Value := GetJsonValue(ConfigContent, 'studio_api_key');
      if Value <> '' then APIKeyEdit.Text := Value;
      
      Value := GetJsonValue(ConfigContent, 'private');
      if Value = 'true' then
        PrivateCheckbox.Checked := True
      else if Value = 'false' then
        PrivateCheckbox.Checked := False;
    end;
  end;
end;

procedure InitializeWizard;
var
  LabelPort, LabelProjects, LabelServerURL, LabelServerName, LabelAPIKey: TNewStaticText;
  DefaultProjectsDir: String;
  TopPos, RowHeight: Integer;
begin
  ConfigPage := CreateCustomPage(wpSelectDir,
    'Server Configuration',
    'Configure your Clustta Studio instance. You can change these later in studio_config.json.');

  DefaultProjectsDir := ExpandConstant('{userdocs}\Clustta Studio');
  TopPos := 0;
  RowHeight := 38;  { Tighter spacing to fit all controls }

  { Port }
  LabelPort := TNewStaticText.Create(ConfigPage);
  LabelPort.Parent := ConfigPage.Surface;
  LabelPort.Caption := 'Port:';
  LabelPort.Top := TopPos;
  LabelPort.Left := 0;

  PortEdit := TNewEdit.Create(ConfigPage);
  PortEdit.Parent := ConfigPage.Surface;
  PortEdit.Top := TopPos + 14;
  PortEdit.Left := 0;
  PortEdit.Width := 100;
  PortEdit.Text := '7774';

  TopPos := TopPos + RowHeight;

  { Projects Directory }
  LabelProjects := TNewStaticText.Create(ConfigPage);
  LabelProjects.Parent := ConfigPage.Surface;
  LabelProjects.Caption := 'Projects Directory:';
  LabelProjects.Top := TopPos;
  LabelProjects.Left := 0;

  ProjectsDirEdit := TNewEdit.Create(ConfigPage);
  ProjectsDirEdit.Parent := ConfigPage.Surface;
  ProjectsDirEdit.Top := TopPos + 14;
  ProjectsDirEdit.Left := 0;
  ProjectsDirEdit.Width := ConfigPage.SurfaceWidth - 90;
  ProjectsDirEdit.Text := DefaultProjectsDir;

  ProjectsDirBrowse := TNewButton.Create(ConfigPage);
  ProjectsDirBrowse.Parent := ConfigPage.Surface;
  ProjectsDirBrowse.Top := TopPos + 12;
  ProjectsDirBrowse.Left := ConfigPage.SurfaceWidth - 85;
  ProjectsDirBrowse.Width := 80;
  ProjectsDirBrowse.Height := 23;
  ProjectsDirBrowse.Caption := 'Browse...';
  ProjectsDirBrowse.OnClick := @BrowseProjectsDir;

  TopPos := TopPos + RowHeight;

  { Server URL (optional, for non-private mode) }
  LabelServerURL := TNewStaticText.Create(ConfigPage);
  LabelServerURL.Parent := ConfigPage.Surface;
  LabelServerURL.Caption := 'Clustta Server URL (leave empty for private mode):';
  LabelServerURL.Top := TopPos;
  LabelServerURL.Left := 0;

  ServerURLEdit := TNewEdit.Create(ConfigPage);
  ServerURLEdit.Parent := ConfigPage.Surface;
  ServerURLEdit.Top := TopPos + 14;
  ServerURLEdit.Left := 0;
  ServerURLEdit.Width := ConfigPage.SurfaceWidth;
  ServerURLEdit.Text := '';

  TopPos := TopPos + RowHeight;

  { Server Name }
  LabelServerName := TNewStaticText.Create(ConfigPage);
  LabelServerName.Parent := ConfigPage.Surface;
  LabelServerName.Caption := 'Studio Name (display name for this studio):';
  LabelServerName.Top := TopPos;
  LabelServerName.Left := 0;

  ServerNameEdit := TNewEdit.Create(ConfigPage);
  ServerNameEdit.Parent := ConfigPage.Surface;
  ServerNameEdit.Top := TopPos + 14;
  ServerNameEdit.Left := 0;
  ServerNameEdit.Width := ConfigPage.SurfaceWidth;
  ServerNameEdit.Text := '';

  TopPos := TopPos + RowHeight;

  { API Key }
  LabelAPIKey := TNewStaticText.Create(ConfigPage);
  LabelAPIKey.Parent := ConfigPage.Surface;
  LabelAPIKey.Caption := 'Studio API Key (required for non-private mode):';
  LabelAPIKey.Top := TopPos;
  LabelAPIKey.Left := 0;

  APIKeyEdit := TNewEdit.Create(ConfigPage);
  APIKeyEdit.Parent := ConfigPage.Surface;
  APIKeyEdit.Top := TopPos + 14;
  APIKeyEdit.Left := 0;
  APIKeyEdit.Width := ConfigPage.SurfaceWidth;
  APIKeyEdit.Text := '';

  TopPos := TopPos + RowHeight;

  { Private Mode }
  PrivateCheckbox := TNewCheckBox.Create(ConfigPage);
  PrivateCheckbox.Parent := ConfigPage.Surface;
  PrivateCheckbox.Top := TopPos;
  PrivateCheckbox.Left := 0;
  PrivateCheckbox.Width := ConfigPage.SurfaceWidth;
  PrivateCheckbox.Caption := 'Private Mode (standalone, no connection to Clustta Cloud)';
  PrivateCheckbox.Checked := True;

  { Load existing config values if upgrading }
  LoadExistingConfig;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;

  if CurPageID = ConfigPage.ID then
  begin
    { Validate port is not empty }
    if Trim(PortEdit.Text) = '' then
    begin
      MsgBox('Please enter a port number.', mbError, MB_OK);
      Result := False;
      Exit;
    end;

    { If not private mode, require API key and server name }
    if not PrivateCheckbox.Checked then
    begin
      if Trim(APIKeyEdit.Text) = '' then
      begin
        MsgBox('Studio API Key is required when not in private mode.', mbError, MB_OK);
        Result := False;
        Exit;
      end;
      if Trim(ServerNameEdit.Text) = '' then
      begin
        MsgBox('Studio Name is required when not in private mode.', mbError, MB_OK);
        Result := False;
        Exit;
      end;
    end;
  end;
end;

function BoolToStr(B: Boolean): String;
begin
  if B then
    Result := 'true'
  else
    Result := 'false';
end;

{ Escape backslashes for JSON string values }
function EscapeJson(const S: String): String;
var
  I: Integer;
begin
  Result := '';
  for I := 1 to Length(S) do
  begin
    if S[I] = '\' then
      Result := Result + '\\'
    else if S[I] = '"' then
      Result := Result + '\"'
    else
      Result := Result + S[I];
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  ConfigFile: String;
  DataDir, ProjectsDir, SharedProjectsDir, StudioUsersDB, SessionDB: String;
  JsonContent: String;
  ExistingContent: AnsiString;
  RegisteredAt: String;
begin
  if CurStep = ssPostInstall then
  begin
    ConfigFile := ExpandConstant('{app}\studio_config.json');

    { Use user-specified projects dir, or default }
    ProjectsDir := Trim(ProjectsDirEdit.Text);
    if ProjectsDir = '' then
      ProjectsDir := ExpandConstant('{userdocs}\Clustta Studio');

    { Derive other paths from install dir }
    DataDir := ExpandConstant('{app}\data');
    SharedProjectsDir := ProjectsDir + '\shared_projects';
    StudioUsersDB := DataDir + '\studio_users.db';
    SessionDB := DataDir + '\sessions.db';

    { Create directories }
    ForceDirectories(ProjectsDir);
    ForceDirectories(SharedProjectsDir);
    ForceDirectories(DataDir);

    { Preserve registered_at from existing config if present }
    RegisteredAt := '';
    if FileExists(ConfigFile) then
    begin
      if LoadStringFromFile(ConfigFile, ExistingContent) then
        RegisteredAt := GetJsonValue(ExistingContent, 'registered_at');
    end;

    { Build JSON config }
    JsonContent :=
      '{' + #13#10 +
      '  "host": "0.0.0.0",' + #13#10 +
      '  "port": "' + Trim(PortEdit.Text) + '",' + #13#10 +
      '  "projects_dir": "' + EscapeJson(ProjectsDir) + '",' + #13#10 +
      '  "shared_projects_dir": "' + EscapeJson(SharedProjectsDir) + '",' + #13#10 +
      '  "server_url": "' + EscapeJson(Trim(ServerURLEdit.Text)) + '",' + #13#10 +
      '  "server_name": "' + EscapeJson(Trim(ServerNameEdit.Text)) + '",' + #13#10 +
      '  "studio_api_key": "' + EscapeJson(Trim(APIKeyEdit.Text)) + '",' + #13#10 +
      '  "studio_users_db": "' + EscapeJson(StudioUsersDB) + '",' + #13#10 +
      '  "session_db": "' + EscapeJson(SessionDB) + '",' + #13#10 +
      '  "private": ' + BoolToStr(PrivateCheckbox.Checked);

    { Add registered_at if it existed }
    if RegisteredAt <> '' then
      JsonContent := JsonContent + ',' + #13#10 + '  "registered_at": "' + RegisteredAt + '"';

    JsonContent := JsonContent + #13#10 + '}';

    { Always write config (user confirmed values via wizard) }
    SaveStringToFile(ConfigFile, JsonContent, False);
  end;
end;
