package state

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/go-chi/chi/v5"
	"github.com/posthog/posthog-go"
	"tangled.sh/tangled.sh/core/api/tangled"
	"tangled.sh/tangled.sh/core/appview"
	"tangled.sh/tangled.sh/core/appview/cache"
	"tangled.sh/tangled.sh/core/appview/cache/session"
	"tangled.sh/tangled.sh/core/appview/config"
	"tangled.sh/tangled.sh/core/appview/db"
	"tangled.sh/tangled.sh/core/appview/notify"
	"tangled.sh/tangled.sh/core/appview/oauth"
	"tangled.sh/tangled.sh/core/appview/pages"
	posthogService "tangled.sh/tangled.sh/core/appview/posthog"
	"tangled.sh/tangled.sh/core/appview/reporesolver"
	"tangled.sh/tangled.sh/core/eventconsumer"
	"tangled.sh/tangled.sh/core/idresolver"
	"tangled.sh/tangled.sh/core/jetstream"
	tlog "tangled.sh/tangled.sh/core/log"
	"tangled.sh/tangled.sh/core/rbac"
	"tangled.sh/tangled.sh/core/tid"
	// xrpcerr "tangled.sh/tangled.sh/core/xrpc/errors"
)

type State struct {
	db            *db.DB
	notifier      notify.Notifier
	oauth         *oauth.OAuth
	enforcer      *rbac.Enforcer
	pages         *pages.Pages
	sess          *session.SessionStore
	idResolver    *idresolver.Resolver
	posthog       posthog.Client
	jc            *jetstream.JetstreamClient
	config        *config.Config
	repoResolver  *reporesolver.RepoResolver
	knotstream    *eventconsumer.Consumer
	spindlestream *eventconsumer.Consumer
}

func Make(ctx context.Context, config *config.Config) (*State, error) {
	d, err := db.Make(config.Core.DbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create db: %w", err)
	}

	enforcer, err := rbac.NewEnforcer(config.Core.DbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create enforcer: %w", err)
	}

	res, err := idresolver.RedisResolver(config.Redis.ToURL())
	if err != nil {
		log.Printf("failed to create redis resolver: %v", err)
		res = idresolver.DefaultResolver()
	}

	pgs := pages.NewPages(config, res)

	cache := cache.New(config.Redis.Addr)
	sess := session.New(cache)

	oauth := oauth.NewOAuth(config, sess)

	posthog, err := posthog.NewWithConfig(config.Posthog.ApiKey, posthog.Config{Endpoint: config.Posthog.Endpoint})
	if err != nil {
		return nil, fmt.Errorf("failed to create posthog client: %w", err)
	}

	repoResolver := reporesolver.New(config, enforcer, res, d)

	wrapper := db.DbWrapper{d}
	jc, err := jetstream.NewJetstreamClient(
		config.Jetstream.Endpoint,
		"appview",
		[]string{
			tangled.GraphFollowNSID,
			tangled.FeedStarNSID,
			tangled.PublicKeyNSID,
			tangled.RepoArtifactNSID,
			tangled.ActorProfileNSID,
			tangled.SpindleMemberNSID,
			tangled.SpindleNSID,
			tangled.StringNSID,
		},
		nil,
		slog.Default(),
		wrapper,
		false,

		// in-memory filter is inapplicalble to appview so
		// we'll never log dids anyway.
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create jetstream client: %w", err)
	}

	ingester := appview.Ingester{
		Db:         wrapper,
		Enforcer:   enforcer,
		IdResolver: res,
		Config:     config,
		Logger:     tlog.New("ingester"),
	}
	err = jc.StartJetstream(ctx, ingester.Ingest())
	if err != nil {
		return nil, fmt.Errorf("failed to start jetstream watcher: %w", err)
	}

	knotstream, err := Knotstream(ctx, config, d, enforcer, posthog)
	if err != nil {
		return nil, fmt.Errorf("failed to start knotstream consumer: %w", err)
	}
	knotstream.Start(ctx)

	spindlestream, err := Spindlestream(ctx, config, d, enforcer)
	if err != nil {
		return nil, fmt.Errorf("failed to start spindlestream consumer: %w", err)
	}
	spindlestream.Start(ctx)

	var notifiers []notify.Notifier
	if !config.Core.Dev {
		notifiers = append(notifiers, posthogService.NewPosthogNotifier(posthog))
	}
	notifier := notify.NewMergedNotifier(notifiers...)

	state := &State{
		d,
		notifier,
		oauth,
		enforcer,
		pgs,
		sess,
		res,
		posthog,
		jc,
		config,
		repoResolver,
		knotstream,
		spindlestream,
	}

	return state, nil
}

func (s *State) Favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=31536000") // one year
	w.Header().Set("ETag", `"favicon-svg-v1"`)

	if match := r.Header.Get("If-None-Match"); match == `"favicon-svg-v1"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	s.pages.Favicon(w)
}

func (s *State) TermsOfService(w http.ResponseWriter, r *http.Request) {
	user := s.oauth.GetUser(r)
	s.pages.TermsOfService(w, pages.TermsOfServiceParams{
		LoggedInUser: user,
	})
}

func (s *State) PrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	user := s.oauth.GetUser(r)
	s.pages.PrivacyPolicy(w, pages.PrivacyPolicyParams{
		LoggedInUser: user,
	})
}

func (s *State) Timeline(w http.ResponseWriter, r *http.Request) {
	user := s.oauth.GetUser(r)

	timeline, err := db.MakeTimeline(s.db)
	if err != nil {
		log.Println(err)
		s.pages.Notice(w, "timeline", "Uh oh! Failed to load timeline.")
	}

	repos, err := db.GetTopStarredReposLastWeek(s.db)
	if err != nil {
		log.Println(err)
		s.pages.Notice(w, "topstarredrepos", "Unable to load.")
		return
	}

	s.pages.Timeline(w, pages.TimelineParams{
		LoggedInUser: user,
		Timeline:     timeline,
		Repos:        repos,
	})
}

func (s *State) Keys(w http.ResponseWriter, r *http.Request) {
	user := chi.URLParam(r, "user")
	user = strings.TrimPrefix(user, "@")

	if user == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	id, err := s.idResolver.ResolveIdent(r.Context(), user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	pubKeys, err := db.GetPublicKeysForDid(s.db, id.DID.String())
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if len(pubKeys) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	for _, k := range pubKeys {
		key := strings.TrimRight(k.Key, "\n")
		w.Write([]byte(fmt.Sprintln(key)))
	}
}

func validateRepoName(name string) error {
	// check for path traversal attempts
	if name == "." || name == ".." ||
		strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("Repository name contains invalid path characters")
	}

	// check for sequences that could be used for traversal when normalized
	if strings.Contains(name, "./") || strings.Contains(name, "../") ||
		strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return fmt.Errorf("Repository name contains invalid path sequence")
	}

	// then continue with character validation
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_' || char == '.') {
			return fmt.Errorf("Repository name can only contain alphanumeric characters, periods, hyphens, and underscores")
		}
	}

	// additional check to prevent multiple sequential dots
	if strings.Contains(name, "..") {
		return fmt.Errorf("Repository name cannot contain sequential dots")
	}

	// if all checks pass
	return nil
}

func stripGitExt(name string) string {
	return strings.TrimSuffix(name, ".git")
}

func (s *State) NewRepo(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		user := s.oauth.GetUser(r)
		knots, err := s.enforcer.GetKnotsForUser(user.Did)
		if err != nil {
			s.pages.Notice(w, "repo", "Invalid user account.")
			return
		}

		s.pages.NewRepo(w, pages.NewRepoParams{
			LoggedInUser: user,
			Knots:        knots,
		})

	case http.MethodPost:
		user := s.oauth.GetUser(r)

		domain := r.FormValue("domain")
		if domain == "" {
			s.pages.Notice(w, "repo", "Invalid form submission&mdash;missing knot domain.")
			return
		}

		repoName := r.FormValue("name")
		if repoName == "" {
			s.pages.Notice(w, "repo", "Repository name cannot be empty.")
			return
		}

		if err := validateRepoName(repoName); err != nil {
			s.pages.Notice(w, "repo", err.Error())
			return
		}

		repoName = stripGitExt(repoName)

		defaultBranch := r.FormValue("branch")
		if defaultBranch == "" {
			defaultBranch = "main"
		}

		description := r.FormValue("description")

		ok, err := s.enforcer.E.Enforce(user.Did, domain, domain, "repo:create")
		if err != nil || !ok {
			s.pages.Notice(w, "repo", "You do not have permission to create a repo in this knot.")
			return
		}

		existingRepo, err := db.GetRepo(s.db, user.Did, repoName)
		if err == nil && existingRepo != nil {
			l.Info("repo exists")
			s.pages.Notice(w, "repo", fmt.Sprintf("You already have a repository by this name on %s", existingRepo.Knot))
			return
		}

		client, err := s.oauth.ServiceClient(
			r,
			oauth.WithService(domain),
			oauth.WithLxm(tangled.RepoCreateNSID),
			oauth.WithDev(s.config.Core.Dev),
		)

		if err != nil {
			s.pages.Notice(w, "repo", "Failed to connect to knot server.")
			return
		}

		rkey := tid.TID()
		repo := &db.Repo{
			Did:         user.Did,
			Name:        repoName,
			Knot:        domain,
			Rkey:        rkey,
			Description: description,
		}

		xrpcClient, err := s.oauth.AuthorizedClient(r)
		if err != nil {
			s.pages.Notice(w, "repo", "Failed to write record to PDS.")
			return
		}

		createdAt := time.Now().Format(time.RFC3339)
		atresp, err := xrpcClient.RepoPutRecord(r.Context(), &comatproto.RepoPutRecord_Input{
			Collection: tangled.RepoNSID,
			Repo:       user.Did,
			Rkey:       rkey,
			Record: &lexutil.LexiconTypeDecoder{
				Val: &tangled.Repo{
					Knot:      repo.Knot,
					Name:      repoName,
					CreatedAt: createdAt,
					Owner:     user.Did,
				}},
		})
		if err != nil {
			log.Printf("failed to create record: %s", err)
			s.pages.Notice(w, "repo", "Failed to announce repository creation.")
			return
		}
		log.Println("created repo record: ", atresp.Uri)

		tx, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			log.Println(err)
			s.pages.Notice(w, "repo", "Failed to save repository information.")
			return
		}
		defer func() {
			tx.Rollback()
			err = s.enforcer.E.LoadPolicy()
			if err != nil {
				log.Println("failed to rollback policies")
			}
		}()

		err = tangled.RepoCreate(
			r.Context(),
			client,
			&tangled.RepoCreate_Input{
				Rkey: rkey,
			},
		)
		if err != nil {
			xe, err := xrpcerr.Unmarshal(err.Error())
			if err != nil {
				log.Println(err)
				s.pages.Notice(w, "repo", "Failed to create repository on knot server.")
				return
			}

			log.Println(xe.Error())
			s.pages.Notice(w, "repo", fmt.Sprintf("Failed to create repository on knot server: %s.", xe.Message))
			return
		}

		err = db.AddRepo(tx, repo)
		if err != nil {
			log.Println(err)
			s.pages.Notice(w, "repo", "Failed to save repository information.")
			return
		}

		// acls
		p, _ := securejoin.SecureJoin(user.Did, repoName)
		err = s.enforcer.AddRepo(user.Did, domain, p)
		if err != nil {
			log.Println(err)
			s.pages.Notice(w, "repo", "Failed to set up repository permissions.")
			return
		}

		err = tx.Commit()
		if err != nil {
			log.Println("failed to commit changes", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		err = s.enforcer.E.SavePolicy()
		if err != nil {
			log.Println("failed to update ACLs", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		s.notifier.NewRepo(r.Context(), repo)

		s.pages.HxLocation(w, fmt.Sprintf("/@%s/%s", user.Handle, repoName))
		return
	}
}
