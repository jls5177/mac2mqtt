package main

import (
	"encoding/json"
	"fmt"
	"github.com/andybrewer/mack"
	"gopkg.in/yaml.v2"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var hostname string

type MuteSyncConfig struct {
	Ip    string `yaml:"mutesync_ip"`
	Port  string `yaml:"mutesync_port"`
	Token string `yaml:"mutesync_token"`
	Valid bool   `yaml:"-"`
}

type config struct {
	Ip             string `yaml:"mqtt_ip"`
	Port           string `yaml:"mqtt_port"`
	User           string `yaml:"mqtt_user"`
	Password       string `yaml:"mqtt_password"`
	Protocol       string `yaml:"mqtt_protocol"`
	MuteSyncConfig `yaml:",inline"`
}

func (c *config) getConfig() *config {

	configContent, err := os.ReadFile("mac2mqtt.yaml")
	if err != nil {
		log.Fatal(err)
	}

	err = yaml.Unmarshal(configContent, c)
	if err != nil {
		log.Fatal(err)
	}

	if c.Ip == "" {
		log.Fatal("Must specify mqtt_ip in mac2mqtt.yaml")
	}

	if c.Port == "" {
		log.Fatal("Must specify mqtt_port in mac2mqtt.yaml")
	}

	if c.User == "" {
		log.Fatal("Must specify mqtt_user in mac2mqtt.yaml")
	}

	if c.Password == "" {
		log.Fatal("Must specify mqtt_password in mac2mqtt.yaml")
	}

	if c.Protocol == "" {
		log.Println("Warning: mqtt_protocol not specified in mac2mqtt.yaml: assuming tcp")
		c.Protocol = "tcp"
	}

	if c.MuteSyncConfig.Token != "" {
		if c.MuteSyncConfig.Ip == "" {
			c.MuteSyncConfig.Ip = "127.0.0.1"
		}
		if c.MuteSyncConfig.Port == "" {
			c.MuteSyncConfig.Port = "8249"
		}
		c.MuteSyncConfig.Valid = true
	} else {
		c.MuteSyncConfig.Valid = false
	}

	return c
}

func (c *config) getBrokerUri() string {
	return fmt.Sprintf("%s://%s:%s", c.Protocol, c.Ip, c.Port)
}

func getHostname() string {

	hostname, err := os.Hostname()

	if err != nil {
		log.Fatal(err)
	}

	// "name.local" => "name"
	firstPart := strings.Split(hostname, ".")[0]

	// remove all symbols, but [a-zA-Z0-9_-]
	reg, err := regexp.Compile("[^a-zA-Z0-9_-]+")
	if err != nil {
		log.Fatal(err)
	}
	firstPart = reg.ReplaceAllString(firstPart, "")

	return firstPart
}

func getCommandOutput(name string, arg ...string) string {
	cmd := exec.Command(name, arg...)

	stdout, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}

	stdoutStr := string(stdout)
	stdoutStr = strings.TrimSuffix(stdoutStr, "\n")

	return stdoutStr
}

func runCommand(name string, arg ...string) {
	cmd := exec.Command(name, arg...)

	_, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}
}

func setMusicVolume(level uint8) {
	fmt.Printf("DEBUG: telling Music to set volume level to %v\n", level)
	_, err := mack.Tell("Music", fmt.Sprintf("set sound volume to %v", level))
	if err != nil {
		fmt.Println(fmt.Errorf("failed to set music volume to %v: %v", level, err))
	}
}

func setMusicPlayPause(play bool) {
	var op string
	if play {
		op = "play"
	} else {
		op = "pause"
	}
	fmt.Printf("DEBUG: telling Music to %v\n", op)
	_, err := mack.Tell("Music", op)
	if err != nil {
		fmt.Println(fmt.Errorf("failed telling Music to %v: %v", op, err))
	}
}

var messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	log.Printf("Received message: %s from topic: %s\n", msg.Payload(), msg.Topic())
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	log.Println("Connected to MQTT")

	mqttClient := NewMQQTClient(client)

	log.Println("Sending 'true' to topic: " + getTopicPrefix() + "/status/alive")
	mqttClient.PublishAndWait("/status/alive", 0, true, "true")

	listen(mqttClient, "/command/#")
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	log.Printf("Disconnected from MQTT: %v\n", err)
}

func NewMQQTClient(client mqtt.Client) *MQQTClient {
	return &MQQTClient{
		client: client,
	}
}

type MQQTMessageHandler func(*MQQTClient, mqtt.Message)

type MQQTClient struct {
	client mqtt.Client
}

func (c *MQQTClient) PublishAndWait(topic string, qos byte, retained bool, msg interface{}) {
	t := c.client.Publish(getTopicPrefix()+topic, qos, retained, msg)
	go func() {
		ok := t.WaitTimeout(1 * time.Second)
		if t.Error() != nil {
			fmt.Println(fmt.Errorf("failed publishing message to topic, %s: %v", topic, t.Error()))
		} else if !ok {
			fmt.Printf(fmt.Sprintf("timed out publishing message to topic, %s", topic))
		}
	}()
}

func (c *MQQTClient) Subscribe(topic string, qos byte, callback MQQTMessageHandler) mqtt.Token {
	return c.client.Subscribe(getTopicPrefix()+topic, qos, func(client mqtt.Client, message mqtt.Message) {
		callback(NewMQQTClient(client), message)
	})
}

func getMQTTClient(broker_uri, user, password, topicPrefix string) *MQQTClient {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker_uri)
	opts.SetClientID(fmt.Sprintf("mac2mqtt-%v", strconv.Itoa(int(uint8(rand.Uint32())))))
	opts.SetUsername(user)
	opts.SetPassword(password)
	opts.OnConnect = connectHandler
	opts.OnConnectionLost = connectLostHandler
	opts.SetOrderMatters(false)
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(true)

	opts.SetWill(getTopicPrefix()+"/status/alive", "false", 0, true)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	return &MQQTClient{
		client: client,
	}
}

func getTopicPrefix() string {
	return "mac2mqtt/" + hostname
}

func listen(client *MQQTClient, topic string) {
	token := client.Subscribe(topic, 0, func(client *MQQTClient, msg mqtt.Message) {
		fmt.Printf("DEBUG: received message: %v\n", msg)
		switch strings.TrimPrefix(msg.Topic(), getTopicPrefix()) {
		case "/command/music/volume":
			i, err := strconv.Atoi(string(msg.Payload()))
			if err == nil && i >= 0 && i <= 100 {
				setMusicVolume(uint8(i))
				updateMusic(client)
			} else {
				log.Println("Incorrect value: " + string(msg.Payload()))
			}
		case "/command/music/playpause":
			b, err := strconv.ParseBool(string(msg.Payload()))
			if err == nil {
				setMusicPlayPause(b)
				updateMusic(client)
			} else {
				log.Println("Incorrect value: " + string(msg.Payload()))
			}
		}

	})

	token.Wait()
	if token.Error() != nil {
		log.Printf("failed subscribing to topic: %s\n", token.Error())
	}
}

type MusicState struct {
	PlayerState string `yaml:"state"`
	TrackId     string `yaml:"trackID"`
	TrackName   string `yaml:"trackName"`
	TrackArtist string `yaml:"trackArtist"`
	Volume      string `yaml:"volume"`
}

func updateMusic(client *MQQTClient) {
	value, err := mack.Tell("Music",
		"set playerState to get player state",
		"set currentTrackID to get id of current track",
		"set currentTrackName to get name of current track",
		"set currentTrackArtist to get artist of current track",
		"set currentVolume to get sound volume",
		"set returnDict to {state:playerState, trackID:currentTrackID, trackArtist:currentTrackArtist, trackName:currentTrackName, volume:currentVolume}",
		"return returnDict",
	)
	if err != nil {
		fmt.Printf("Error: %v", err)
		return
	}
	//fmt.Printf("DEBUG: %v\n", value)

	// convert output to Yaml to parse using standard library methods
	tmpYaml := strings.ReplaceAll(value, ", ", "\n")
	tmpYaml = strings.ReplaceAll(tmpYaml, ":", ": ")
	var musicState MusicState
	if err := yaml.Unmarshal([]byte(tmpYaml), &musicState); err != nil {
		fmt.Printf("Error: %v", err)
		return
	}

	// publish each record to the broker
	client.PublishAndWait("/status/music/volume", 0, false, musicState.Volume)
	client.PublishAndWait("/status/music/state", 0, false, musicState.PlayerState)
	client.PublishAndWait("/status/music/trackID", 0, false, musicState.TrackId)
	client.PublishAndWait("/status/music/trackName", 0, false, musicState.TrackName)
	client.PublishAndWait("/status/music/trackArtist", 0, false, musicState.TrackArtist)
}

func getBatteryChargePercent() string {

	output := getCommandOutput("/usr/bin/pmset", "-g", "batt")
	//fmt.Printf("DEBUG: battery output: %v\n", output)

	// $ /usr/bin/pmset -g batt
	// Now drawing from 'Battery Power'
	//  -InternalBattery-0 (id=4653155)        100%; discharging; 20:00 remaining present: true

	r := regexp.MustCompile(`(\d+)%`)
	percent := r.FindStringSubmatch(output)[1]

	return percent
}

func updateBattery(client *MQQTClient) {
	client.PublishAndWait("/status/battery", 0, false, getBatteryChargePercent())
}

type MuteSync struct {
	Hostname  string `json:"hostname"`
	InMeeting bool   `json:"in_meeting"`
	Muted     bool   `json:"muted"`
	UserId    string `json:"user-id"`
}

type MuteSyncResponse struct {
	Data MuteSync `json:"Data"`
}

func updateMuteSync(client *MQQTClient, config *MuteSyncConfig) {
	muteSyncUri := fmt.Sprintf("http://%s:%s/state", config.Ip, config.Port)
	req, err := http.NewRequest(http.MethodGet, muteSyncUri, nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)
	req.Header.Add("Accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("Error: making http request: %s\n", err)
		return
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("client: could not read response body: %s\n", err)
		return
	}

	var muteSync MuteSyncResponse
	if err := json.Unmarshal(resBody, &muteSync); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	//fmt.Printf("DEBUG-MuteSync: %+v\n", muteSync)
	client.PublishAndWait("/status/mutesync/inMeeting", 0, false, strconv.FormatBool(muteSync.Data.InMeeting))
	client.PublishAndWait("/status/mutesync/muted", 0, false, strconv.FormatBool(muteSync.Data.Muted))
}

func main() {

	log.Println("Started")

	var c config
	c.getConfig()

	var wg sync.WaitGroup

	hostname = getHostname()
	mqttClient := getMQTTClient(c.getBrokerUri(), c.User, c.Password, getTopicPrefix())

	musicTicker := time.NewTicker(2 * time.Second)
	batteryTicker := time.NewTicker(60 * time.Second)

	wg.Add(1)
	go func() {
		for {
			select {
			case _ = <-musicTicker.C:
				updateMusic(mqttClient)
				if c.MuteSyncConfig.Valid {
					updateMuteSync(mqttClient, &c.MuteSyncConfig)
				}

			case _ = <-batteryTicker.C:
				updateBattery(mqttClient)
			}
		}
	}()

	wg.Wait()

}
