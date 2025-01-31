package aquagram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type PollingOptions struct {
	Offset             int           `json:"offset,omitempty"`
	Limit              int           `json:"limit,omitempty"`
	Timeout            time.Duration `json:"-"`
	TimeoutRaw         int64         `json:"timeout,omitempty"`
	AllowedUpdates     []string      `json:"allowed_updates,omitempty"`
	DropPendingUpdates bool          `json:"-"`
}

type Updates struct {
	Result []*Update `json:"result"`
}

type PollingUpdater struct {
	Bot     *Bot
	Options *PollingOptions
}

func NewPollingUpdater(bot *Bot) *PollingUpdater {
	updater := new(PollingUpdater)
	updater.Bot = bot

	return updater
}

func (updater *PollingUpdater) Start() {
	if updater.Options == nil {
		updater.Options = new(PollingOptions)
	}

	if updater.Options.Timeout.Seconds() == 0 {
		updater.Options.Timeout = 10 * time.Second
	}

	if updater.Options.DropPendingUpdates {
		params := &PollingOptions{
			Offset: -1,
			Limit:  1,
		}

		updates, err := updater.Bot.GetUpdates(updater.Bot.stopContext, params)
		if err != nil {
			updater.Bot.Logger.Println(fmt.Errorf("%w: %w", ErrUpdaterError, err))

		} else {
			if len(updates) > 0 {
				lastUpdate := updates[0]
				updater.Options.Offset = lastUpdate.UpdateID + 1
			}
		}
	}

	for {
		updates, err := updater.Bot.GetUpdates(updater.Bot.stopContext, updater.Options)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				updater.Bot.Logger.Println("bot manually stopped")
				break
			}

			updater.Bot.Logger.Println(fmt.Errorf("%w: %w", ErrUpdaterError, err))
			continue
		}

		for _, update := range updates {
			updater.Options.Offset = update.UpdateID + 1
			go updater.Bot.DispatchUpdate(update)
		}
	}
}

func (bot *Bot) GetUpdates(ctx context.Context, params *PollingOptions) ([]*Update, error) {
	params.TimeoutRaw = int64(params.Timeout.Seconds())

	data, err := bot.Raw(ctx, "getUpdates", params)
	if err != nil {
		return nil, err
	}

	updates := new(Updates)

	if err := json.Unmarshal(data, updates); err != nil {
		return nil, err
	}

	return updates.Result, nil
}

type WebhookUpdater struct {
	Bot         *Bot
	secretToken string
}

func NewWebhookUpdater(bot *Bot) *WebhookUpdater {
	updater := new(WebhookUpdater)
	updater.Bot = bot

	return updater
}

func (updater *WebhookUpdater) Start(addr string) error {
	router := http.NewServeMux()
	router.HandleFunc("/", updater.Handler)

	return http.ListenAndServe(addr, router)
}

func (updater *WebhookUpdater) Handler(w http.ResponseWriter, r *http.Request) {
	if updater.secretToken != EmptyString {
		secretToken := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")

		if secretToken != updater.secretToken {
			return
		}
	}

	update := new(Update)

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(update); err != nil {
		updater.Bot.Logger.Println(fmt.Errorf("%w: %w", ErrUpdaterError, err))
		return
	}

	if update.UpdateID <= updater.Bot.LastUpdateID {
		return
	}

	go updater.Bot.DispatchUpdate(update)
}
