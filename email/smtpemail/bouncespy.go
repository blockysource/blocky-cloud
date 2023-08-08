// Copyright 2023 The Blocky Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// This file was based on the github.com/erizocosmico/go-bouncespy package with the license:
// The MIT License (MIT)
//
// Copyright (c) 2015 Miguel Molina
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package smtpemail

import (
	"fmt"
	"strings"
)

// bounceType defines the type of the bounce, that is, hard or soft
type bounceType int

const (
	hardBounce bounceType = 1
	softBounce bounceType = 2
)

// bounceReason is a status code that tells why the message was bounced according to
// https://tools.ietf.org/html/rfc3463#section-3 and https://tools.ietf.org/html/rfc821#section-4.2.2
type bounceReason string

const (
	serviceNotAvailable               bounceReason = "421"
	mailActionNotTaken                bounceReason = "450"
	actionAbortedErrorProcessing      bounceReason = "451"
	actionAbortedInsufficientStorage  bounceReason = "452"
	cmdSyntaxError                    bounceReason = "500"
	argumentsSyntaxError              bounceReason = "501"
	cmdNotImplemented                 bounceReason = "502"
	badCmdSequence                    bounceReason = "503"
	cmdParamNotImplemented            bounceReason = "504"
	mailboxUnavailable                bounceReason = "550"
	recipientNotLocal                 bounceReason = "551"
	actionAbortedExceededStorageAlloc bounceReason = "552"
	mailboxNameInvalid                bounceReason = "553"
	transactionFailed                 bounceReason = "554"

	addressDoesntExist                       bounceReason = "5.0.0"
	otherAddressError                        bounceReason = "5.1.0"
	badDestinationMailboxAddress             bounceReason = "5.1.1"
	badDestinationSystemAddress              bounceReason = "5.1.2"
	badDestinationMailboxAddressSyntax       bounceReason = "5.1.3"
	destinationMailboxAmbiguous              bounceReason = "5.1.4"
	destinationMailboxAddressInvalid         bounceReason = "5.1.5"
	mailboxMoved                             bounceReason = "5.1.6"
	badSenderMailboxAddressSyntax            bounceReason = "5.1.7"
	badSenderSystemAddress                   bounceReason = "5.1.8"
	undefinedMailboxError                    bounceReason = "5.2.0"
	mailboxDisabled                          bounceReason = "5.2.1"
	mailboxFull                              bounceReason = "5.2.2"
	messageLenExceedsLimit                   bounceReason = "5.2.3"
	mailingListExpansionProblem              bounceReason = "5.2.4"
	undefinedMailSystemStatus                bounceReason = "5.3.0"
	mailSystemFull                           bounceReason = "5.3.1"
	systemNotAcceptingNetworkMessages        bounceReason = "5.3.2"
	systemNotCapableOfFeatures               bounceReason = "5.3.3"
	messageTooBigForSystem                   bounceReason = "5.3.4"
	undefinedNetworkStatus                   bounceReason = "5.4.0"
	noAnswerFromHost                         bounceReason = "5.4.1"
	badConnection                            bounceReason = "5.4.2"
	routingServerFailure                     bounceReason = "5.4.3"
	unableToRoute                            bounceReason = "5.4.4"
	networkCongestion                        bounceReason = "5.4.5"
	routingLoopDetected                      bounceReason = "5.4.6"
	deliveryTimeExpired                      bounceReason = "5.4.7"
	undefinedProtocolStatus                  bounceReason = "5.5.0"
	invalidCommand                           bounceReason = "5.5.1"
	syntaxError                              bounceReason = "5.5.2"
	tooManyRecipients                        bounceReason = "5.5.3"
	invalidCommandArguments                  bounceReason = "5.5.4"
	wrongProtocolVersion                     bounceReason = "5.5.5"
	undefinedMediaError                      bounceReason = "5.6.0"
	mediaNotSupported                        bounceReason = "5.6.1"
	conversionRequiredAndProhibited          bounceReason = "5.6.2"
	conversionRequiredButNotSupported        bounceReason = "5.6.3"
	conversionWithLossPerformed              bounceReason = "5.6.4"
	conversionFailed                         bounceReason = "5.6.5"
	undefinedSecurityStatus                  bounceReason = "5.7.0"
	messageRefused                           bounceReason = "5.7.1"
	mailingListExpansionProhibited           bounceReason = "5.7.2"
	securityConversionRequiredButNotPossible bounceReason = "5.7.3"
	securityFeaturesNotSupported             bounceReason = "5.7.4"
	cryptoFailure                            bounceReason = "5.7.5"
	cryptoAlgorithmNotSupported              bounceReason = "5.7.6"
	messageIntegrityFailure                  bounceReason = "5.7.7"
	undefinedCode                            bounceReason = "9.1.1"

	// bounceCodeNotFound means we did not found the reason in the email
	bounceCodeNotFound bounceReason = ""
)

// bounceStatusMap is a map indexed by bounce reason that returns an object with
// its bounce type and whether it's an specific error or not (an enhanced)
var bounceStatusMap = map[bounceReason]struct {
	Type     bounceType
	Specific bool
}{
	serviceNotAvailable:               {softBounce, false},
	mailActionNotTaken:                {softBounce, false},
	actionAbortedErrorProcessing:      {softBounce, false},
	actionAbortedInsufficientStorage:  {softBounce, false},
	cmdSyntaxError:                    {hardBounce, false},
	argumentsSyntaxError:              {hardBounce, false},
	cmdNotImplemented:                 {hardBounce, false},
	badCmdSequence:                    {hardBounce, false},
	cmdParamNotImplemented:            {hardBounce, false},
	mailboxUnavailable:                {hardBounce, false},
	recipientNotLocal:                 {hardBounce, false},
	actionAbortedExceededStorageAlloc: {hardBounce, false},
	mailboxNameInvalid:                {hardBounce, false},
	transactionFailed:                 {hardBounce, false},

	addressDoesntExist:                       {hardBounce, true},
	otherAddressError:                        {hardBounce, true},
	badDestinationMailboxAddress:             {hardBounce, true},
	badDestinationSystemAddress:              {hardBounce, true},
	badDestinationMailboxAddressSyntax:       {hardBounce, true},
	destinationMailboxAmbiguous:              {hardBounce, true},
	destinationMailboxAddressInvalid:         {hardBounce, true},
	mailboxMoved:                             {hardBounce, true},
	badSenderMailboxAddressSyntax:            {hardBounce, true},
	badSenderSystemAddress:                   {hardBounce, true},
	undefinedMailboxError:                    {softBounce, true},
	mailboxDisabled:                          {softBounce, true},
	mailboxFull:                              {softBounce, true},
	messageLenExceedsLimit:                   {hardBounce, true},
	mailingListExpansionProblem:              {hardBounce, true},
	undefinedMailSystemStatus:                {hardBounce, true},
	mailSystemFull:                           {softBounce, true},
	systemNotAcceptingNetworkMessages:        {hardBounce, true},
	systemNotCapableOfFeatures:               {hardBounce, true},
	messageTooBigForSystem:                   {hardBounce, true},
	undefinedNetworkStatus:                   {hardBounce, true},
	noAnswerFromHost:                         {hardBounce, true},
	badConnection:                            {hardBounce, true},
	routingServerFailure:                     {hardBounce, true},
	unableToRoute:                            {hardBounce, true},
	networkCongestion:                        {softBounce, true},
	routingLoopDetected:                      {hardBounce, true},
	deliveryTimeExpired:                      {hardBounce, true},
	undefinedProtocolStatus:                  {hardBounce, true},
	invalidCommand:                           {hardBounce, true},
	syntaxError:                              {hardBounce, true},
	tooManyRecipients:                        {softBounce, true},
	invalidCommandArguments:                  {hardBounce, true},
	wrongProtocolVersion:                     {hardBounce, true},
	undefinedMediaError:                      {hardBounce, true},
	mediaNotSupported:                        {hardBounce, true},
	conversionRequiredAndProhibited:          {hardBounce, true},
	conversionRequiredButNotSupported:        {hardBounce, true},
	conversionWithLossPerformed:              {hardBounce, true},
	conversionFailed:                         {hardBounce, true},
	undefinedSecurityStatus:                  {hardBounce, true},
	messageRefused:                           {hardBounce, true},
	mailingListExpansionProhibited:           {hardBounce, true},
	securityConversionRequiredButNotPossible: {hardBounce, true},
	securityFeaturesNotSupported:             {hardBounce, true},
	cryptoFailure:                            {hardBounce, true},
	cryptoAlgorithmNotSupported:              {hardBounce, true},
	messageIntegrityFailure:                  {hardBounce, true},
	undefinedCode:                            {hardBounce, true},
}

var reasonDescriptions = map[bounceReason]string{
	serviceNotAvailable:               "service not available",
	mailActionNotTaken:                "mail action not taken: mailbox unavailable",
	actionAbortedErrorProcessing:      "action aborted: error in processing",
	actionAbortedInsufficientStorage:  "action aborted: insufficient system storage",
	cmdSyntaxError:                    "the server could not recognize the command due to a syntax error",
	argumentsSyntaxError:              "a syntax error was encountered in command arguments",
	cmdNotImplemented:                 "this command is not implemented",
	badCmdSequence:                    "the server has encountered a bad sequence of commands",
	cmdParamNotImplemented:            "a command parameter is not implemented",
	mailboxUnavailable:                "user's mailbox was unavailable (such as not found)",
	recipientNotLocal:                 "the recipient is not local to the server",
	actionAbortedExceededStorageAlloc: "the action was aborted due to exceeded storage allocation",
	mailboxNameInvalid:                "the command was aborted because the mailbox name is invalid",
	transactionFailed:                 "the transaction failed for some unstated reason",

	addressDoesntExist:                       "address does not exist",
	otherAddressError:                        "other address status",
	badDestinationMailboxAddress:             "bad destination mailbox address",
	badDestinationSystemAddress:              "bad destination system address",
	badDestinationMailboxAddressSyntax:       "bad destunation mailbox address syntax",
	destinationMailboxAmbiguous:              "destination mailbox address ambiguous",
	destinationMailboxAddressInvalid:         "destination mailbox address invalid",
	mailboxMoved:                             "mailbox has moved",
	badSenderMailboxAddressSyntax:            "bad sender's mailbox address syntax",
	badSenderSystemAddress:                   "bad sender's system address",
	undefinedMailboxError:                    "other or undefined mailbox status",
	mailboxDisabled:                          "mailbox disabled, not accepting messages",
	mailboxFull:                              "mailbox full",
	messageLenExceedsLimit:                   "message length exceeds administrative limit",
	mailingListExpansionProblem:              "mailing list expansion problem",
	undefinedMailSystemStatus:                "other or undefined mail system status",
	mailSystemFull:                           "mail system full",
	systemNotAcceptingNetworkMessages:        "system not accepting network messages",
	systemNotCapableOfFeatures:               "system not capable of selected features",
	messageTooBigForSystem:                   "message too big for system",
	undefinedNetworkStatus:                   "other or undefined network or routing status",
	noAnswerFromHost:                         "no answer from host",
	badConnection:                            "bad connection",
	routingServerFailure:                     "routing server failure",
	unableToRoute:                            "unable to route",
	networkCongestion:                        "network congestion",
	routingLoopDetected:                      "routing loop detected",
	deliveryTimeExpired:                      "delivery time expired",
	undefinedProtocolStatus:                  "other or undefined protocol status",
	invalidCommand:                           "invalid command",
	syntaxError:                              "syntax error",
	tooManyRecipients:                        "too many recipients",
	invalidCommandArguments:                  "invalid command arguments",
	wrongProtocolVersion:                     "wrong protocol version",
	undefinedMediaError:                      "other or undefined media error",
	mediaNotSupported:                        "media not supported",
	conversionRequiredAndProhibited:          "conversion required and prohibited",
	conversionRequiredButNotSupported:        "conversion required but not supported",
	conversionWithLossPerformed:              "conversion with loss performed",
	conversionFailed:                         "conversion failed",
	undefinedSecurityStatus:                  "other or undefined security status",
	messageRefused:                           "delivery not authorized, message refused",
	mailingListExpansionProhibited:           "mailing list expansion prohibited",
	securityConversionRequiredButNotPossible: "security conversion required but nor possible",
	securityFeaturesNotSupported:             "security features not supported",
	cryptoFailure:                            "cryptographic failure",
	cryptoAlgorithmNotSupported:              "cryptographic algorithm not supported",
	messageIntegrityFailure:                  "message integrity failure",
	undefinedCode:                            "hard bounce with no bounce code found",
}

const (
	lessSpecific = -1
	moreSpecific = 1
	equal        = 0
	BothNotFound = -2
)

// compare returns a comparison code depending on the relation between the two
// reasons to compare.
//
// Let A be the compared reason and B the reason to compare it to
// - If A and B are bounceCodeNotFound, the result is BothNotFound
// - If A and B are both enhanced reasons, the result is equal
// - If A is enhanced but B is not, the result is moreSpecific
// - If As is not enhanced but B is, the result is lessSpecific
func (r bounceReason) compare(o bounceReason) int {
	if r == bounceCodeNotFound && o == bounceCodeNotFound {
		return BothNotFound
	}

	infoSelf := bounceStatusMap[r]
	infoOther := bounceStatusMap[o]
	if infoSelf.Specific == infoOther.Specific {
		return equal
	} else if infoSelf.Specific {
		return moreSpecific
	} else {
		return lessSpecific
	}
}

// String returns the status code of the reason plus the human
// readable description of it
func (r bounceReason) String() string {
	if r == bounceCodeNotFound {
		return "no bounce reason found"
	}

	return fmt.Sprintf(
		"%s - %s",
		string(r),
		reasonDescriptions[r],
	)
}

const (
	errorOtherServerReturned  = "the error that the other server returned was:"
	reasonOfTheProblem        = "the reason of the problem:"
	reasonForTheProblem       = "the reason for the problem:"
	deliveryFailedPermanently = "delivery to the following recipient failed permanently:"
	deliveryDelayed           = "delivery to the following recipient has been delayed:"
)

func analyzeLine(line string) bounceReason {
	var firstStatus, secondStatus bounceReason
	parts := strings.Split(removeUnnecessaryChars(line), " ")

	if len(parts) > 1 {
		secondStatus = parseStatus(parts[1])
	}

	if len(parts) > 0 {
		firstStatus = parseStatus(parts[0])
	}

	switch firstStatus.compare(secondStatus) {
	case lessSpecific:
		return secondStatus
	case moreSpecific, equal:
		return firstStatus
	default:
		return bounceCodeNotFound
	}
}

func parseStatus(status string) bounceReason {
	status = strings.TrimSpace(status)
	if _, ok := bounceStatusMap[bounceReason(status)]; ok {
		return bounceReason(status)
	}
	return bounceCodeNotFound
}

func removeUnnecessaryChars(line string) string {
	return removeSpaces(removeSpaces(removeDashes(line)))
}

var statusReplacer = strings.NewReplacer(
	"-", " ",
	"  ", " ",
)

func removeDashes(line string) string {
	return strings.Replace(line, "-", " ", -1)
}

func removeSpaces(line string) string {
	return strings.Replace(line, "  ", " ", -1)
}
