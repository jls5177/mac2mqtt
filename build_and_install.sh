#!/usr/bin/env bash

# enable unofficial bash strict mode
set -euo pipefail
IFS=$'\n\t'

echo "building tools"
go build -o ~/bin/mac2mqtt/mac2mqtt .

if [[ -e mac2mqtt.yaml ]]; then
  echo "installing latest YAML file"
  cp mac2mqtt.yaml ~/bin/mac2mqtt/
fi

INSTALL_DIR="${INSTALL_DIR:-$HOME/bin/mac2mqtt}"

createLaunchFile() {
  cat > $1 <<-EOF
<plist version="1.0">
    <dict>
        <key>Label</key>
        <string>com.bessarabov.mac2mqtt</string>
        <key>Program</key>
        <string>$INSTALL_DIR/mac2mqtt</string>
        <key>WorkingDirectory</key>
        <string>$INSTALL_DIR/</string>
        <key>EnvironmentVariables</key>
        <dict>
            <key>PATH</key>
            <string>/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
        </dict>
        <key>RunAtLoad</key>
        <true/>
        <key>KeepAlive</key>
        <true/>
        <key>StandardOutPath</key>
        <string>$INSTALL_DIR/mac2mqtt.out.log</string>
        <key>StandardErrorPath</key>
        <string>$INSTALL_DIR/mac2mqtt.err.log</string>
        <key>ProcessType</key>
        <string>Interactive</string>
        <key>Nice</key>
        <integer>-20</integer>
    </dict>
</plist>
EOF
}

# restart launchctl
if [[ -f $HOME/Library/LaunchAgents/com.bessarabov.mac2mqtt.plist ]]; then
  echo "attempting to stop agent (if running)"
  $(launchctl unload $HOME/Library/LaunchAgents/com.bessarabov.mac2mqtt.plist)
fi

echo "Creating launch agent file"
createLaunchFile $HOME/Library/LaunchAgents/com.bessarabov.mac2mqtt.plist

echo "Launching mac2mqtt"
launchctl load $HOME/Library/LaunchAgents/com.bessarabov.mac2mqtt.plist
