package service

import (
	"bytes"
	"fmt"
	"text/template"
)

const (
	// launchdLabel identifica el LaunchDaemon; el plist se llama <label>.plist.
	launchdLabel = "com.argos.agent"
	// macUser es el usuario de sistema dedicado (convencion macOS: prefijo "_").
	macUser = "_argos"
	// macLogPath recibe stdout/stderr (JSON de slog) del daemon.
	macLogPath = "/var/log/argos-agent.log"
)

// El plist va con Umask 63 (= 0077 en octal; launchd lo pide en decimal) y sin
// KeepAlive agresivo: solo se relanza si sale con error, y no antes de 30 s.
var launchdPlist = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.ExecPath}}</string>
		<string>run</string>
		<string>--service</string>
	</array>
	<key>UserName</key>
	<string>{{.User}}</string>
	<key>GroupName</key>
	<string>{{.User}}</string>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>30</integer>
	<key>ProcessType</key>
	<string>Background</string>
	<key>Umask</key>
	<integer>63</integer>
	<key>WorkingDirectory</key>
	<string>/var/empty</string>
	<key>StandardOutPath</key>
	<string>{{.LogPath}}</string>
	<key>StandardErrorPath</key>
	<string>{{.LogPath}}</string>
</dict>
</plist>
`))

// RenderLaunchdPlist genera el plist del LaunchDaemon pa el binario instalado en execPath.
func RenderLaunchdPlist(execPath string) (string, error) {
	if err := safePath(execPath); err != nil {
		return "", err
	}
	var b bytes.Buffer
	err := launchdPlist.Execute(&b, struct{ Label, ExecPath, User, LogPath string }{launchdLabel, execPath, macUser, macLogPath})
	if err != nil {
		return "", fmt.Errorf("generando el plist: %w", err)
	}
	return b.String(), nil
}
