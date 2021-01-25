package ghmon

import (
	"log"
	"sort"
	"time"
)

type ScoreCalculator struct {
	logger *log.Logger
}

type LoggerConsole struct {
	logger *log.Logger
}

func  (scoreCalculator *ScoreCalculator) pullRequestReviewStatusToInt(pullRequestReview *PullRequestReview) int {
	switch pullRequestReview.Status {
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
		return scoreCalculator.pullRequestReviewStatusToInt(pullRequestReviews[i]) < scoreCalculator.pullRequestReviewStatusToInt(pullRequestReviews[j])
	})

	return pullRequestReviews[0]

}

func (scoreCalculator *ScoreCalculator) CalculateTotalScore(pullRequestScore PullRequestScore) int {

	if pullRequestScore.ChangesRequested > 0 {
		return -1
	}

	if pullRequestScore.Approvals == pullRequestScore.NumReviewers {
		return 100
	}

	if pullRequestScore.Comments > 0 {
		return 50
	}

	return 150
}


func (scoreCalculator *ScoreCalculator) CalculateScore(pullRequestWrapper *PullRequestWrapper) PullRequestScore {

	pullRequestScore := PullRequestScore{}

	importantPullRequestReviews := make([]*PullRequestReview, 0)
	for _, pullRequestReviews := range pullRequestWrapper.PullRequest.PullRequestReviewsByUser {
		pullRequestReview := scoreCalculator.ExtractMostImportantFirst(pullRequestReviews)
		importantPullRequestReviews = append(importantPullRequestReviews, pullRequestReview)
	}

	pullRequestScore.NumReviewers = uint(len(importantPullRequestReviews))
	pullRequestScore.Seen = pullRequestWrapper.Seen
	pullRequestScore.AgeSec = uint32(time.Now().Unix() - pullRequestWrapper.FirstSeen.Unix())

	for _, pullRequestReview := range importantPullRequestReviews {
		switch pullRequestReview.Status {
		case PullRequestReviewStatusApproved : pullRequestScore.Approvals++
		case PullRequestReviewStatusChangesRequested: pullRequestScore.ChangesRequested++
		case PullRequestReviewStatusCommented : pullRequestScore.Comments++
		}
	}

	return pullRequestScore
}

func (scoreCalculator *ScoreCalculator) RankPullRequestReview(left *PullRequestReview, right *PullRequestReview) int {
	leftScore := scoreCalculator.pullRequestReviewStatusToInt(left)
	rightScore := scoreCalculator.pullRequestReviewStatusToInt(right)
	return leftScore - rightScore
}