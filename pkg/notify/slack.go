package notify

import (
	"context"
	"github.com/armory-io/bilgepump/pkg/cache"
	"github.com/armory-io/bilgepump/pkg/config"
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/nlopes/slack"
	"github.com/sirupsen/logrus"
	"regexp"
	"time"
)

var emailCheck = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")

const MAX_SLACK_ATTACHMENTS = 20 // the API technically supports 100 but after 20 they're "unreadable" according to Slack

type SlackNotifier struct {
	config       *config.Config
	logger       *logrus.Logger
	client       *slack.Client
	ctx          context.Context
	cache        cache.Cache
	defaultOwner *slack.User
}

func NewSlackNotifier(ctx context.Context, cfg *config.Config, logger *logrus.Logger, cache cache.Cache) *SlackNotifier {

	client := slack.New(cfg.Slack.Token)

	sl := &SlackNotifier{
		ctx:    ctx,
		config: cfg,
		logger: logger,
		client: client,
		cache:  cache,
	}

	return sl
}

func (sn *SlackNotifier) findUserByEmail(email string) *slack.User {
	if emailCheck.MatchString(email) {
		user, err := sn.client.GetUserByEmailContext(sn.ctx, email)
		if err != nil {
			sn.logger.Error(err)
			return nil
		}
		sn.logger.Debug("found user email: ", user)
		return user
	}
	return nil
}

func (sn *SlackNotifier) findUserByName(user string) *slack.User {
	users, err := sn.client.GetUsersContext(sn.ctx)
	if err != nil {
		sn.logger.Error(err)
		return nil
	}
	for _, u := range users {
		if u.Name == user {
			sn.logger.Debug("found user: ", u.ID)
			return &u
		}
	}
	return nil
}

func (sn *SlackNotifier) IsValid() bool {
	byEmail := sn.findUserByEmail(sn.config.Slack.DefaultOwner)
	byUser := sn.findUserByName(sn.config.Slack.DefaultOwner)
	if byEmail == nil && byUser == nil {
		sn.logger.Error("Cannot find a default slack user to notify")
		return false
	}

	if byEmail != nil {
		sn.defaultOwner = byEmail
	}

	if byUser != nil {
		sn.defaultOwner = byUser
	}
	return true
}

func (sn *SlackNotifier) Collect() {
	// determine if we can match a slack username or email.  if we can't assets owned by that owner default to the default_owner
	notifyTargets := map[string]*slack.User{}
	notifyTargets[""] = sn.defaultOwner
	owners, err := sn.cache.ReadOwners()
	if err != nil {
		sn.logger.Error(err)
		return
	}
	for _, o := range owners {
		if o != "" {
			email := sn.findUserByEmail(o)
			user := sn.findUserByName(o)
			switch {
			case email != nil:
				notifyTargets[o] = email
				continue
			case user != nil:
				notifyTargets[o] = user
				continue
			default:
				notifyTargets[o] = sn.defaultOwner
				continue
			}
		}
	}

	sn.logger.Debug(notifyTargets)
	// we've got owners, send the assets collected by owner to the person that needs to know we're going to ruin their world
	for _, o := range owners {
		c := sn.cache.ReadCandidates(o)
		sn.logger.Debug(len(c))
		// convert candidates into MarkedCandidates and pass into Send()
		mcs, err := mark.BuildCandidates(o, sn.cache)
		if err != nil {
			sn.logger.Error(err)
			return
		}
		err = sn.SlackSend(notifyTargets[o], mcs)
		if err != nil {
			sn.logger.Error(err)
		}
	}
}

func (sn *SlackNotifier) Send() error {
	return nil
}

func (sn *SlackNotifier) SlackSend(user *slack.User, candidate []*mark.MarkedCandidate) error {
	attachments := make([]slack.Attachment, len(candidate))

	for i, c := range candidate {
		attachment := slack.Attachment{
			Color:  c.MarkerType.Color(),
			Text:   c.Id,
			Fields: c.GenerateSlackAttachmentFields(),
		}
		attachments[i] = attachment
	}
	// send to default channel if one exists
	id := user.ID
	if sn.config.Slack.Channel != "" && user == sn.defaultOwner {
		id = sn.config.Slack.Channel
	}
	// chunk the sends to slack
	attSize := len(attachments)
	var chunkedAttachments [][]slack.Attachment
	for i := 0; i < attSize; i += MAX_SLACK_ATTACHMENTS {
		chunk := i + MAX_SLACK_ATTACHMENTS
		if chunk > attSize {
			chunk = attSize
		}
		chunkedAttachments = append(chunkedAttachments, attachments[i:chunk])
	}
	for _, attachmentChunk := range chunkedAttachments {
		channelID, timestamp, err := sn.client.PostMessage(id, slack.MsgOptionText("Instances that have expiring ttl", false),
			slack.MsgOptionAttachments(attachmentChunk...), slack.MsgOptionAsUser(true))
		time.Sleep(time.Second * 2) // we sleep one second to avoid rate limiting and having Bilge become potentially banned
		if err != nil {
			sn.logger.Error(err)
			continue
		}
		sn.logger.Debugf("Message successfully sent to channel %s at %s", channelID, timestamp)
	}

	return nil
}
