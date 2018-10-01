package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/nlopes/slack"
	"github.com/spf13/cobra"
)

var defaultSupportChannels = []string{
	"customersupport",
	"frame",
	"monitoring",
	"sentry",
}

var slackKey string
var dryRun bool

var rootCmd = &cobra.Command{
	Use:   "roachslack [command] (flags)",
	Short: "roachslack is a tool for joining and removing support channels via the command line",
	Long: `
Examples:
	roachprod joinSupport --key="YOUR SLACK AUTH TOKEN"
	roachprod leaveSupport --key="YOUR SLACK AUTH TOKEN" --dry=true
`,
}

func wrap(f func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		err := f(cmd, args)
		if err != nil {
			cmd.Println("Error: ", err.Error())
			os.Exit(1)
		}
	}
}

func checkAuth(ctx context.Context) (*slack.Client, error) {
	if len(slackKey) == 0 {
		return nil, fmt.Errorf("no slack auth key provided")
	}

	api := slack.New(slackKey)

	// Check that the user is signed in with a real token.
	authResp, err := api.AuthTestContext(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Logged in as: %s\n\tTeam: %s\n", authResp.User, authResp.Team)
	return api, nil
}

func getAllChannels(ctx context.Context, client *slack.Client) ([]slack.Channel, error) {
	var channels []slack.Channel
	var cursor string
	for {
		params := &slack.GetConversationsParameters{
			Cursor:          cursor,
			ExcludeArchived: "true",
		}
		channelPage, nextCursor, err := client.GetConversationsContext(ctx, params)
		if err != nil {
			return nil, err
		}
		channels = append(channels, channelPage...)
		if len(nextCursor) == 0 {
			break
		}
		cursor = nextCursor
	}
	return channels, nil
}

var joinSuppportCmd = &cobra.Command{
	Use:   "joinSupport --key=\"xxx\"",
	Short: "join all cockroach support channels",
	Args:  cobra.NoArgs,
	Run: wrap(func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		client, err := checkAuth(ctx)
		if err != nil {
			return err
		}

		channels, err := getAllChannels(ctx, client)
		if err != nil {
			return err
		}

		var supportChannelNames []string
		channelIDs := make(map[string]string)
		for _, channel := range channels {
			// Don't join channels you already belong to.
			if channel.IsMember {
				continue
			}

			// Add all customer channels
			if strings.HasPrefix(channel.Name, "_") {
				supportChannelNames = append(supportChannelNames, channel.Name)
				channelIDs[channel.Name] = channel.ID
				continue
			}

			// Add default support channels.
			for _, defaultChannel := range defaultSupportChannels {
				if defaultChannel == channel.Name {
					supportChannelNames = append(supportChannelNames, channel.Name)
					channelIDs[channel.Name] = channel.ID
					continue
				}
			}
		}

		fmt.Printf("\n--------------------\n")

		if len(supportChannelNames) == 0 {
			fmt.Printf("There are no support channels left for you to join.\n")
			return nil
		}

		sort.Strings(supportChannelNames)
		fmt.Printf("You will be joining the following channels:\n")
		for _, channelName := range supportChannelNames {
			fmt.Printf("%s\n", channelName)
		}

		if dryRun {
			fmt.Printf("\n--------------------\n")
			fmt.Printf("Dry run only, no joins were performed.\n")
			return nil
		}

		// Join the channels.
		for _, channelName := range supportChannelNames {
			if _, err := client.JoinChannelContext(ctx, channelName); err != nil {
				return err
			}
			fmt.Printf("Joined %s\n", channelName)
		}

		// Marking the joined channels as read.
		fmt.Printf("\n--------------------\n")
		fmt.Printf("Marking all the joined channels as read.\n")
		time.Sleep(5 * time.Second)
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		fmt.Printf("%s\n", timestamp)
		for _, channelName := range supportChannelNames {
			channelID := channelIDs[channelName]
			if err := client.SetChannelReadMarkContext(ctx, channelID, timestamp); err != nil {
				return err
			}
			fmt.Printf("%s is marked as read.\n", channelName)
		}

		fmt.Printf("\n--------------------\n")
		fmt.Printf("Done!\n\n")

		return nil
	}),
}

var leaveSuppportCmd = &cobra.Command{
	Use:   "leaveSupport --key=\"xxx\"",
	Short: "join all cockroach client support channels (not main customer support ones)",
	Args:  cobra.NoArgs,
	Run: wrap(func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		client, err := checkAuth(ctx)
		if err != nil {
			return err
		}

		channels, err := getAllChannels(ctx, client)
		if err != nil {
			return err
		}

		var supportChannelNames []string
		var cannotLeave []string
		channelIDs := make(map[string]string)
		for _, channel := range channels {
			// Don't join channels you already belong to.
			if !channel.IsMember {
				continue
			}

			// Add all customer channels
			if strings.HasPrefix(channel.Name, "_") {
				// Sadly, externally shared channels cannot be left via the API as far as I can tell.
				if channel.IsExtShared {
					cannotLeave = append(cannotLeave, channel.Name)
					continue
				}
				supportChannelNames = append(supportChannelNames, channel.Name)
				channelIDs[channel.Name] = channel.ID
				continue
			}
		}

		fmt.Printf("\n--------------------\n")

		if len(supportChannelNames) == 0 {
			fmt.Printf("There are no support channels left for you to leave.\n")
			return nil
		}

		sort.Strings(supportChannelNames)
		fmt.Printf("You will be leaving the following channels:\n")
		for _, channelName := range supportChannelNames {
			fmt.Printf("%s\n", channelName)
		}

		if dryRun {
			fmt.Printf("\n--------------------\n")
			fmt.Printf("The following shared channels must be left manually:\n")
			for _, channel := range cannotLeave {
				fmt.Printf("%s\n", channel)
			}
			fmt.Printf("\nDry run only, no exits were performed.\n")
			return nil
		}

		fmt.Printf("\n--------------------\n")
		for _, channelName := range supportChannelNames {
			channelID := channelIDs[channelName]
			if _, err := client.LeaveChannelContext(ctx, channelID); err != nil {
				return err
			}
			fmt.Printf("Left %s\n", channelName)
		}

		fmt.Printf("\n--------------------\n")

		fmt.Printf("The following shared channels must be left manually:\n")
		for _, channel := range cannotLeave {
			fmt.Printf("%s\n", channel)
		}

		fmt.Printf("\n--------------------\n")
		fmt.Printf("Done!\n\n")

		return nil
	}),
}

func main() {
	// The commands are displayed in the order they are added to rootCmd. Note
	// that gcCmd and adminurlCmd contain a trailing \n in their Short help in
	// order to separate the commands into logical groups.
	cobra.EnableCommandSorting = false
	rootCmd.AddCommand(
		joinSuppportCmd,
		leaveSuppportCmd,
	)

	rootCmd.PersistentFlags().StringVarP(
		&slackKey, "key", "k", os.Getenv("SLACK_KEY"),
		"Slack API Key: See https://api.slack.com/custom-integrations/legacy-tokens",
	)

	rootCmd.PersistentFlags().BoolVarP(
		&dryRun, "dry", "d", false,
		"Perform a dry run only, don't change any settings",
	)

	if err := rootCmd.Execute(); err != nil {
		// Cobra has already printed the error message.
		os.Exit(1)
	}
}
