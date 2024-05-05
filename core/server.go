package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

type SearchEngine interface {
	Search(Query) ([]SearchResult, error)
	SearchImage(Query) ([]SearchResult, error)
	IsInitialized() bool
	Name() string
	GetRateLimiter() *rate.Limiter
}

type Server struct {
	app  *fiber.App
	addr string
}

func NewServer(host string, port int, searchEngines ...SearchEngine) *Server {
	addr := fmt.Sprintf("%s:%d", host, port)
	serv := Server{
		app:  fiber.New(),
		addr: addr,
	}

	for _, engine := range searchEngines {
		locEngine := engine
		limiter := engine.GetRateLimiter()

		serv.app.Get(fmt.Sprintf("/%s/search", strings.ToLower(locEngine.Name())), func(c *fiber.Ctx) error {
			q := Query{}
			err := q.InitFromContext(c)
			if err != nil {
				logrus.Errorf("Error while setting %s query: %s", locEngine.Name(), err)
				return err
			}

			err = limiter.Wait(context.Background())
			if err != nil {
				logrus.Errorf("Ratelimiter error during %s query: %s", locEngine.Name(), err)
			}

			res, err := locEngine.Search(q)
			if err != nil {
				switch err {
				case ErrCaptcha:
					err = fmt.Errorf("captcha found, please stop sending requests for a while\n%s", err)
				case ErrSearchTimeout:
					err = fmt.Errorf("%s", err)
				}

				logrus.Errorf("Error during %s search: %s", locEngine.Name(), err)
				return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
			}

			return c.JSON(res)
		})

		serv.app.Get(fmt.Sprintf("/%s/image", strings.ToLower(locEngine.Name())), func(c *fiber.Ctx) error {
			q := Query{}
			err := q.InitFromContext(c)
			if err != nil {
				logrus.Errorf("Error while setting %s query: %s", locEngine.Name(), err)
				return err
			}

			err = limiter.Wait(context.Background())
			if err != nil {
				logrus.Errorf("Ratelimiter error during %s query: %s", locEngine.Name(), err)
			}

			res, err := locEngine.SearchImage(q)

			if err != nil && len(res) > 0 {
				c.Status(503)
				return c.JSON(res)
			}

			if err != nil {
				switch err {
				case ErrCaptcha:
					err = fmt.Errorf("captcha found, please stop sending requests for a while: %s", err)
				case ErrSearchTimeout:
					err = fmt.Errorf("%s", err)
				}

				logrus.Errorf("Error during %s search: %s", locEngine.Name(), err)
				return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
			}

			return c.JSON(res)
		})
	}

	return &serv
}

func (s *Server) Listen() error {
	return s.app.Listen(s.addr)
}

func (s *Server) Shutdown() error {
	return s.app.Shutdown()
}
