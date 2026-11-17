package openshift

import (
	"fmt"
	"regexp"

	semver "github.com/blang/semver/v4"
	"github.com/charmbracelet/lipgloss"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	msgOperatorAlreadyInstalled = "%s is already installed and ready"
	latestInChannelLabel        = "(latest in channel)"
	specField                   = "spec"
	channelField                = "channel"
	startingCSVField            = "startingCSV"
	nameField                   = "name"
	colorYellow                 = "3"
	colorGreen                  = "2"
)

// upgradeVersions holds the current and available version information for display.
type upgradeVersions struct {
	currentChannel    string
	currentCSV        string
	availableChannel  string
	availableCSV      string
	availableCSVLabel string
}

// upgradePromptDetails holds display text and confirmation prompt for an upgrade kind.
type upgradePromptDetails struct {
	actionText    string
	actionTitle   string
	confirmPrompt string
}

type upgradeKind int

const (
	kindSame upgradeKind = iota
	kindUpgrade
	kindDowngrade
	kindChannelSwitch
)

// versionRe matches the semver portion of an OLM CSV name or channel name, e.g.
//
//	rhods-operator.3.5.0   → "3.5.0"
//	spyre-operator.v1.3.1  → "v1.3.1"
//	stable-3.5             → "3.5"
//	stable-v1.3            → "v1.3"
//	fast-v1                → "v1"
var versionRe = regexp.MustCompile(`(?:^|[-._])v?(\d+(?:\.\d+)*(?:[-+].*)?)$`)

// extractVersion extracts a comparable semver from an OLM CSV or channel name.
// Returns a zero version and false if the name carries no recognisable semver.
func extractVersion(name string) (semver.Version, bool) {
	m := versionRe.FindStringSubmatch(name)
	if len(m) == 0 {
		return semver.Version{}, false
	}

	v, err := semver.ParseTolerant(m[1])
	if err != nil {
		return semver.Version{}, false
	}

	return v, true
}

// extractUpgradeVersions pulls version fields from the subscription and desired manifest.
func extractUpgradeVersions(sub *operatorsv1alpha1.Subscription, desired *unstructured.Unstructured) upgradeVersions {
	desiredChannel, _, _ := unstructured.NestedString(desired.Object, specField, channelField)
	desiredCSV, _, _ := unstructured.NestedString(desired.Object, specField, startingCSVField)

	label := desiredCSV
	if label == "" {
		label = latestInChannelLabel
	}

	return upgradeVersions{
		currentChannel:    sub.Spec.Channel,
		currentCSV:        sub.Status.InstalledCSV,
		availableChannel:  desiredChannel,
		availableCSV:      desiredCSV,
		availableCSVLabel: label,
	}
}

// compareSemverStrings parses and compares two semver strings.
// Returns the comparison kind and true if both versions were successfully parsed.
func compareSemverStrings(current, available string) (upgradeKind, bool) {
	if available == "" {
		return kindSame, false
	}

	curVer, curOK := extractVersion(current)
	availVer, availOK := extractVersion(available)
	if !curOK || !availOK {
		return kindSame, false
	}

	if availVer.GT(curVer) {
		return kindUpgrade, true
	}
	if availVer.LT(curVer) {
		return kindDowngrade, true
	}

	return kindSame, true
}

// isChannelDowngradedAgainstCSV checks if the available channel is older than the current CSV major.minor.
func isChannelDowngradedAgainstCSV(curCSV, availChan string) bool {
	curCSVVer, curCSVOK := extractVersion(curCSV)
	availChanVer, availChanOK := extractVersion(availChan)
	if !curCSVOK || !availChanOK {
		return false
	}

	availMajorMinor := semver.Version{Major: availChanVer.Major, Minor: availChanVer.Minor}
	curMajorMinor := semver.Version{Major: curCSVVer.Major, Minor: curCSVVer.Minor}

	return availMajorMinor.LT(curMajorMinor)
}

// compareVersions determines the relationship between the currently installed version
// and the available version in the desired manifest.
func compareVersions(v upgradeVersions) upgradeKind {
	if v.currentChannel == v.availableChannel && (v.availableCSV == "" || v.currentCSV == v.availableCSV) {
		return kindSame
	}

	if kind, ok := compareSemverStrings(v.currentCSV, v.availableCSV); ok && kind != kindSame {
		return kind
	}

	if kind, ok := compareSemverStrings(v.currentChannel, v.availableChannel); ok && kind != kindSame {
		return kind
	}

	if isChannelDowngradedAgainstCSV(v.currentCSV, v.availableChannel) {
		return kindDowngrade
	}

	if v.currentChannel != v.availableChannel {
		return kindChannelSwitch
	}

	return kindSame
}

// printUpgradeTable renders a table showing current vs available versions.
func printUpgradeTable(v upgradeVersions, title string) {
	logger.Infoln(lipgloss.NewStyle().Bold(true).Render("\n" + title))

	printer := utils.NewTableWriter()
	defer printer.CloseTableWriter()

	printer.SetCollapseIndices()
	setUpgradeTableHeaders(printer)
	populateUpgradeRows(printer, v)
}

func setUpgradeTableHeaders(printer *utils.Printer) {
	printer.SetHeaders("", "CHANNEL", "VERSION (CSV)")
}

func populateUpgradeRows(printer *utils.Printer, v upgradeVersions) {
	currentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorYellow))
	availableStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorGreen))

	printer.AppendRow(
		currentStyle.Render("Current"),
		currentStyle.Render(v.currentChannel),
		currentStyle.Render(v.currentCSV),
	)
	printer.AppendRow(
		availableStyle.Render("Available"),
		availableStyle.Render(v.availableChannel),
		availableStyle.Render(v.availableCSVLabel),
	)
}

func getUpgradePromptDetails(kind upgradeKind, operatorLabel string) upgradePromptDetails {
	if kind == kindChannelSwitch {
		return upgradePromptDetails{
			actionText:    "channel update",
			actionTitle:   "Channel update available: " + operatorLabel,
			confirmPrompt: fmt.Sprintf("\nUpdate channel for %s?", operatorLabel),
		}
	}

	return upgradePromptDetails{
		actionText:    "upgrade",
		actionTitle:   "Upgrade available: " + operatorLabel,
		confirmPrompt: fmt.Sprintf("\nUpgrade %s?", operatorLabel),
	}
}

func confirmUpgrade(v upgradeVersions, details upgradePromptDetails, operatorLabel string, s *spinner.Spinner) (bool, error) {
	s.Stop(fmt.Sprintf("%s %s available", operatorLabel, details.actionText))
	printUpgradeTable(v, details.actionTitle)

	confirmed, err := utils.ConfirmAction(details.confirmPrompt)
	if err != nil {
		return false, fmt.Errorf("confirmation failed for %s: %w", operatorLabel, err)
	}

	if !confirmed {
		logger.Infof("Skipping %s for %s", details.actionText, operatorLabel)

		return false, nil
	}

	logger.Infof("Proceeding with %s for %s", details.actionText, operatorLabel)

	return true, nil
}

// promptAndUpgrade shows the current/available version table, prompts the user,
// and returns (true, nil) if confirmed so caller proceeds with the server-side apply.
// Returns (false, nil) if skipped (caller should treat the subscription as already handled).
func promptAndUpgrade(sub *operatorsv1alpha1.Subscription, desired *unstructured.Unstructured, operatorLabel string, s *spinner.Spinner) (bool, error) {
	v := extractUpgradeVersions(sub, desired)
	kind := compareVersions(v)

	switch kind {
	case kindSame:
		s.Stop(fmt.Sprintf(msgOperatorAlreadyInstalled, operatorLabel))

		return false, nil

	case kindDowngrade:
		logger.Debugf("%s: skipping, manifest channel/version is older than installed (current: %s/%s, available: %s/%s)\n",
			operatorLabel, v.currentChannel, v.currentCSV, v.availableChannel, v.availableCSV)
		s.Stop(fmt.Sprintf(msgOperatorAlreadyInstalled, operatorLabel))

		return false, nil

	case kindUpgrade, kindChannelSwitch:
		details := getUpgradePromptDetails(kind, operatorLabel)

		return confirmUpgrade(v, details, operatorLabel, s)

	default:
		return false, nil
	}
}

// isTargetCSVReached checks if the installed CSV meets the target or has changed from the previous CSV.
func isTargetCSVReached(installedCSV, targetCSV, previousCSV string) bool {
	if installedCSV == "" {
		return false
	}

	if targetCSV != "" {
		if installedCSV == targetCSV {
			return true
		}

		targetVer, targetOK := extractVersion(targetCSV)
		installedVer, installedOK := extractVersion(installedCSV)
		if targetOK && installedOK && installedVer.GTE(targetVer) {
			return true
		}

		return false
	}

	if previousCSV != "" {
		return installedCSV != previousCSV
	}

	return true
}
