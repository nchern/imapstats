package main

import (
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_configDefaultBehaviours(t *testing.T) {
	cfg, err := fetchConfig("testdata/not-exists.yaml")
	require.NoError(t, err)
	assert.Empty(t, cfg.Accounts)

	statCfg := cfg.getStatsCfg("foo", "bar")
	assert.Equal(t, statsConfig{"unseen_count": &criteriaCfg{}}, statCfg)
}

func Test_fetchConfigShouldFailOnInvalidOrClause(t *testing.T) {
	cfg, err := fetchConfig("testdata/config.invalid-or.yaml")
	require.EqualError(t, err, "bad config: OR criteria must have 2 clauses")
	assert.Nil(t, cfg)
}

func Test_fetchConfigShouldLoadFile(t *testing.T) {
	var tests = []struct {
		expected statsConfig
		given    string
	}{
		{
			statsConfig{
				"important_count": &criteriaCfg{
					Headers: map[string]string{
						"From": "boss@bar.com",
					},
				},
				"notification_count": &criteriaCfg{
					Headers: map[string]string{
						"Subject": "Notification:",
					},
					Body: []string{"foo", "bar"},
				},
				"seen_count": &criteriaCfg{Seen: true},
			},
			"testdata/config.yaml",
		},
		{
			statsConfig{
				"important_count": &criteriaCfg{
					Headers: map[string]string{
						"From": "boss@bar.com",
					},
					Body: []string{"foo", "bar"},
					Or: []criteriaCfg{
						{
							Headers: map[string]string{
								"Subject": "blah",
							},
							Body: []string{"fuzz"},
						},
						{
							Headers: map[string]string{
								"Subject": "foo",
							},
						},
					},
				},
			},
			"testdata/config.with-or.yaml",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.given, func(t *testing.T) {
			underTest, err := fetchConfig(tt.given)
			require.NoError(t, err)
			require.NotNil(t, underTest)

			actual := underTest.Accounts["foo@bar.com"]["INBOX"]
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func Test_criteriaCfgToIMAP(t *testing.T) {
	actual := &criteriaCfg{
		Headers: map[string]string{
			"From":    "foo@bar.com",
			"Subject": "hello",
		},
		Body: []string{"foo", "bar"},
	}
	expected := imap.NewSearchCriteria()
	expected.WithoutFlags = []string{imap.SeenFlag}
	expected.Body = []string{"foo", "bar"}
	expected.Header.Add("From", "foo@bar.com")
	expected.Header.Add("Subject", "hello")
	assert.Equal(t, expected, actual.toIMAP())

	// test defaults
	actual = &criteriaCfg{}
	expected = imap.NewSearchCriteria()
	expected.WithoutFlags = []string{imap.SeenFlag}
	assert.Equal(t, expected, actual.toIMAP())
}

func Test_criteriaCfgToIMAPShouldPanicOnASingleCriterion(t *testing.T) {
	given := &criteriaCfg{
		Or: []criteriaCfg{
			{Seen: true},
		},
	}
	assert.PanicsWithValue(t, "OR criteria can't have 1 criterion", func() { given.toIMAP() })
}

func Test_criteriaCfgToIMAPShouldHanldleORClauseWithTwoCriteria(t *testing.T) {
	given := &criteriaCfg{
		Or: []criteriaCfg{
			{Headers: map[string]string{"Subject": "foo"}},
			{Headers: map[string]string{"Subject": "bar"}},
		},
	}

	first := imap.NewSearchCriteria()
	first.Header.Add("Subject", "foo")
	first.WithoutFlags = []string{imap.SeenFlag}

	second := imap.NewSearchCriteria()
	second.Header.Add("Subject", "bar")
	second.WithoutFlags = []string{imap.SeenFlag}

	expected := imap.NewSearchCriteria()
	expected.WithoutFlags = []string{imap.SeenFlag}
	expected.Or = [][2]*imap.SearchCriteria{
		{first, second},
	}
	assert.Equal(t, expected, given.toIMAP())
}

func Test_criteriaCfgToIMAPShouldHanldleORClauseWithMoreThanTwoCriteria(t *testing.T) {
	given := &criteriaCfg{
		Or: []criteriaCfg{
			{Headers: map[string]string{"Subject": "foo"}},
			{Headers: map[string]string{"Subject": "bar"}},
			{Headers: map[string]string{"Subject": "fuzz"}},
		},
	}

	leafR := imap.NewSearchCriteria()
	leafR.Header.Add("Subject", "bar")
	leafR.WithoutFlags = []string{imap.SeenFlag}

	leafL := imap.NewSearchCriteria()
	leafL.Header.Add("Subject", "fuzz")
	leafL.WithoutFlags = []string{imap.SeenFlag}

	first := imap.NewSearchCriteria()
	first.Header.Add("Subject", "foo")
	first.WithoutFlags = []string{imap.SeenFlag}

	second := imap.NewSearchCriteria()
	second.Or = [][2]*imap.SearchCriteria{
		{leafR, leafL},
	}

	expected := imap.NewSearchCriteria()
	expected.WithoutFlags = []string{imap.SeenFlag}
	expected.Or = [][2]*imap.SearchCriteria{
		{first, second},
	}
	assert.Equal(t, expected, given.toIMAP())
}

func Test_cacheTTL(t *testing.T) {
	assert.Equal(t, ttlInfinite, cacheTTL())

	var tests = []struct {
		expected time.Duration
		given    string
	}{
		{81 * time.Second, "81"},
		{10 * time.Second, "10s"},
		{15 * time.Minute, "15m"},
		{33 * time.Hour, "33h"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.given, func(t *testing.T) {
			*ttlArg = tt.given
			assert.Equal(t, tt.expected, cacheTTL())
		})
	}
}
