package main

import (
	"testing"

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

func Test_fetchConfigShouldLoadFile(t *testing.T) {
	underTest, err := fetchConfig("testdata/config.yaml")
	require.NoError(t, err)
	require.NotNil(t, underTest)

	expected := statsConfig{
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
	}
	actual := underTest.Accounts["foo@bar.com"]["INBOX"]
	assert.Equal(t, expected, actual)
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