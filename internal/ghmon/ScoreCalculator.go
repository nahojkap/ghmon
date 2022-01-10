package ghmon

import (
	"log"
	"math"
	"sort"
	"time"
)

type ScoreCalculator struct {
	user *User
	logger *log.Logger
}

type LoggerConsole struct {
	logger *log.Logger
}

func  (scoreCalculator *ScoreCalculator) PullRequestReviewStatusToInt(status PullRequestReviewStatus) int {
	switch status {
	case PullRequestReviewStatusApproved:
		return 12
	case PullRequestReviewStatusCommented:
		return 15
	case PullRequestReviewStatusChangesRequested:
		return 10
	case PullRequestReviewStatusPending:
		return 17
	case PullRequestReviewStatusRequested:
		return 20
	case PullRequestReviewStatusDismissed:
		// Dismissed is a little vague - is it approved -> changes to code -> dismissed approval?
		return 13
	default:
		return 50
	}
}

func (scoreCalculator *ScoreCalculator) ExtractMostImportantFirst(pullRequestReviews []*PullRequestReview) *PullRequestReview {

	// FIXME: The reviews need to be sorted by date as well - e.g. its possible to have an approval *after* a comment
	// FIXME: and then another comment, which officially requires a re-review
	// FIXME: Or should it be that the presence of any approval should be signalled differently and only be
	// FIXME: cancelled if there is a request for change?

	sort.Slice(pullRequestReviews, func (i, j int) bool {
		if (pullRequestReviews[i].Status == PullRequestReviewStatusApproved || pullRequestReviews[i].Status == PullRequestReviewStatusChangesRequested) && (pullRequestReviews[j].Status == PullRequestReviewStatusApproved || pullRequestReviews[j].Status == PullRequestReviewStatusChangesRequested) {
			return pullRequestReviews[i].SubmittedAt.After(pullRequestReviews[j].SubmittedAt)
		}
		return scoreCalculator.PullRequestReviewStatusToInt(pullRequestReviews[i].Status) < scoreCalculator.PullRequestReviewStatusToInt(pullRequestReviews[j].Status)
	})

	return pullRequestReviews[0]

}

func (scoreCalculator *ScoreCalculator) CalculateTotalScore(user *User, pullRequestWrapper *PullRequestWrapper) float32 {

	if pullRequestWrapper.Deleted {
		return -999
	}

	pullRequestScore := pullRequestWrapper.Score

	if pullRequestScore.Dismissed > 0 {
		return -10
	}

	//
	// * Is prioritized project
	// * Is prioritized creator?
	// * Has X minutes passed since 'seen'?
	// * Was it seen and never 'opened'?
	// * Has it been all approved already?
	//   * If 'own', then has it been a a while since it was approved?
	//   * is the current user one of the approvers?

	// Has the PR been all approved?
	//if pullRequestScore.Approvals == pullRequestScore.NumReviewers {
	//	return 0
	//}

	var totalScore float32 = 0

	if pullRequestScore.ChangesRequested > 0 {
		totalScore += 75
	}

	if pullRequestScore.Approvals > 0 {
		totalScore -= float32(pullRequestScore.Approvals * 10)
	}

	if pullRequestScore.Comments > 0 {
		totalScore += float32(pullRequestScore.Comments * 10)
	}

	if pullRequestScore.Approvals == pullRequestScore.NumReviewers {
		// Other peoples pull requests that are fully approved are less important
		if !pullRequestScore.IsMyPullRequest {
			totalScore -= 100
		} else {
			// Own pull requests that are approved but not yet merged get high priority!
			totalScore += 50
		}
	}

	if pullRequestScore.IsMyPullRequest {

		if pullRequestScore.AgeSec > (60 * 60 * 5) {
			totalScore += 50
		} else {
			totalScore += 25
		}

	} else {

		if pullRequestScore.ApprovedByMe {
			totalScore -= 200
		} else {
			if pullRequestScore.AgeSec > (60*60*48) {
				totalScore += 50
			} else if pullRequestScore.AgeSec > (60*60*24) {
				totalScore += 30
			} else if pullRequestScore.AgeSec > (60*60*6) {
				totalScore += 20
			} else if pullRequestScore.AgeSec > (60*60) {
				totalScore += 10
			}
		}

	}

	return float32(math.Min(float64(100), float64(totalScore)))
}


func (scoreCalculator *ScoreCalculator) CalculateScore(user *User, pullRequestWrapper *PullRequestWrapper) PullRequestScore {

	pullRequestScore := PullRequestScore{}

	importantPullRequestReviews := make([]*PullRequestReview, 0)
	for _, pullRequestReviews := range pullRequestWrapper.PullRequest.PullRequestReviewsByUser {
		pullRequestReview := scoreCalculator.ExtractMostImportantFirst(pullRequestReviews)
		importantPullRequestReviews = append(importantPullRequestReviews, pullRequestReview)
	}

	pullRequestScore.IsMyPullRequest = pullRequestWrapper.PullRequest.Creator.Id == user.Id

	pullRequestScore.NumReviewers = uint(len(importantPullRequestReviews))
	pullRequestScore.Seen = pullRequestWrapper.Seen
	pullRequestScore.AgeSec = uint32(time.Now().Unix() - pullRequestWrapper.FirstSeen.Unix())

	for _, pullRequestReview := range importantPullRequestReviews {
		switch pullRequestReview.Status {
		case PullRequestReviewStatusApproved :
			pullRequestScore.Approvals++
			pullRequestScore.ApprovedByMe = pullRequestReview.User.Id == user.Id
		case PullRequestReviewStatusChangesRequested: pullRequestScore.ChangesRequested++
		case PullRequestReviewStatusCommented : pullRequestScore.Comments++
		}
	}

	pullRequestScore.Total = scoreCalculator.CalculateTotalScore(user, pullRequestWrapper)

	return pullRequestScore
}

func (scoreCalculator *ScoreCalculator) RankPullRequestReview(left *PullRequestReview, right *PullRequestReview) int {
	leftScore := scoreCalculator.PullRequestReviewStatusToInt(left.Status)
	rightScore := scoreCalculator.PullRequestReviewStatusToInt(right.Status)
	return leftScore - rightScore
}