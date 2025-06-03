package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const (
	prefix       = ".mb"
	mention      = "<@1322018283108700190>"
	firebase_url = "https://autopenguin-go-default-rtdb.firebaseio.com/data.json"
)

type Config struct {
	Channel string   `json:"channel"`
	Users   []string `json:"users"`
}

var client = openai.NewClient(
	option.WithBaseURL("https://api.pawan.krd/cosmosrp/v1"),
	option.WithAPIKey("my-pawan-key"),
)

func ask(prompt string, user string) string {

	chatCompletion, err := client.Chat.Completions.New(context.TODO(), openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are MagZ Bot, a discord bot (but you act like a penguin) that chats with users. You are in the server of MagZ Bem, a youtuber and streamer whose profile is a penguin. He streams and makes youtube shorts about Minecraft. You ALWAYS respond in short messages. You are always polite and friendly. You are not for helping users with issues, and if they ask you, tell them to reach out to server mods and admins. You always follow the server rules, that are: 1. Respectful Communication, 2. No Inappropriate Content, 3. Stay On Topic, 4. No Spam or Self-Promotion, 5. Follow Discord's Terms of Service, 6. No Impersonation"),
			openai.UserMessage(fmt.Sprintf("prompt: %s user: %s", prompt, user)),
		},
		Model: openai.ChatModelGPT4o,
	})
	if err != nil {
		log.Fatal(err)
	}

	return chatCompletion.Choices[0].Message.Content
}

func getConfig() (*Config, error) {
	resp, err := http.Get(firebase_url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var config Config
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func saveConfig(config *Config) error {
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, firebase_url, bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func getID(id string) (string, error) {
	if strings.HasPrefix(id, "<@") && strings.HasSuffix(id, ">") {
		id = strings.TrimPrefix(id, "<@")
		id = strings.TrimSuffix(id, ">")
		return id, nil
	}
	return "", fmt.Errorf("invalid id")
}

func handleMessage(s *discordgo.Session, m *discordgo.Message) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Get current configuration
	config, err := getConfig()
	if err != nil {
		log.Println("Failed to fetch config:", err)
		return
	}

	// Check if the user is in the allowed list
	userInList := false
	for _, id := range config.Users {
		if id == m.Author.ID {
			userInList = true
			break
		}
	}

	if !userInList {
		pattern := `\[[^\]]+\]\([^)]+\)`
		matched, _ := regexp.MatchString(pattern, m.Content)
		if matched {
			s.ChannelMessageDelete(m.ChannelID, m.ID)
			channel, err := s.UserChannelCreate(m.Author.ID)
			if err != nil {
				log.Println(err)
				return
			}
			s.ChannelMessageSend(channel.ID, "You cannot post edited links in this server. Please send the original link.")
			return
		}
	}

	args := strings.Split(m.Content, " ")

	if args[0] == mention {
		message := strings.TrimPrefix(m.Content, mention)
		message = strings.TrimSpace(message)

		if message == "" {
			return
		}

		fmt.Println("User:", m.Author.Username, "Message:", message)

		response := ask(message, m.Author.Username)
		s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
			Content: response,
			Reference: &discordgo.MessageReference{
				MessageID: m.ID,
				ChannelID: m.ChannelID,
				GuildID:   m.GuildID,
			},
		})

		return

	}

	if len(args) < 2 || args[0] != prefix {
		return
	}

	switch args[1] {
	case "ping":
		message_time := m.Timestamp
		pong_time := time.Now()
		time_to_ping := pong_time.Sub(message_time)
		time_to_ping = time_to_ping.Truncate(time.Millisecond)
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Title:       "Pong!",
			Description: "Your Ping got Pong'd! " + time_to_ping.String(),
			Color:       0x00ff00,
		})

	case "adduser":
		if !userInList {
			return
		}
		if len(args) != 3 {
			s.ChannelMessageSend(m.ChannelID, "Usage: "+prefix+" adduser <@user>")
			return
		}
		id, err := getID(args[2])
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, "Invalid user format.")
			return
		}
		for _, uid := range config.Users {
			if uid == id {
				s.ChannelMessageSend(m.ChannelID, "User already added.")
				return
			}
		}
		config.Users = append(config.Users, id)
		if err := saveConfig(config); err != nil {
			s.ChannelMessageSend(m.ChannelID, "Failed to save config.")
		} else {
			s.ChannelMessageSend(m.ChannelID, "User added successfully.")
		}

	case "listusers":
		if !userInList {
			return
		}
		var list string
		for _, id := range config.Users {
			user, err := s.User(id)
			if err != nil {
				log.Fatal(err)
			}
			list += user.Username + "\n"
		}
		s.ChannelMessageSend(m.ChannelID, list)

	case "setchannel":
		if !userInList {
			return
		}
		if len(args) != 3 {
			s.ChannelMessageSend(m.ChannelID, "Usage: "+prefix+" setchannel <#channel>")
			return
		}
		id := strings.TrimPrefix(args[2], "<#")
		id = strings.TrimSuffix(id, ">")
		config.Channel = id
		if err := saveConfig(config); err != nil {
			s.ChannelMessageSend(m.ChannelID, "Failed to update channel.")
		} else {
			s.ChannelMessageSend(m.ChannelID, "Channel updated successfully.")
		}
	}
}

func main() {
	sess, err := discordgo.New("Bot " + "API_KEY_HERE")
	if err != nil {
		log.Fatal(err)
	}

	sess.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMessageReactions |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsGuilds |
		discordgo.IntentsMessageContent

	sess.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		handleMessage(s, m.Message)
	})

	sess.AddHandler(func(s *discordgo.Session, m *discordgo.MessageUpdate) {
		if m.Content != "" {
			handleMessage(s, m.Message)
		}
	})

	sess.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
		config, err := getConfig()
		if err != nil || config.Channel == "" {
			log.Println("Cannot welcome user:", err)
			return
		}
		embed := discordgo.MessageEmbed{
			Title:       fmt.Sprintf("Welcome to the server, %s!", m.User.Username),
			Description: "Please read the rules.",
			Color:       0x00ff00,
			Image:       &discordgo.MessageEmbedImage{URL: "attachment://welcome.png"},
		}
		s.ChannelMessageSendEmbed(config.Channel, &embed)
	})

	go func() {
		for {
			err := sess.Open()
			if err != nil {
				log.Println("Failed to connect to Discord:", err)
				time.Sleep(10 * time.Second)
				continue
			}
			log.Println("Connected to Discord.")
			break
		}
	}()
	defer sess.Close()

	fmt.Println("Bot is running!")

	go func() {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "Bot is alive!")
		})
		log.Fatal(http.ListenAndServe(":"+port, nil))
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-stop
}
