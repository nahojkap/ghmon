package ghmon
import (
	"github.com/robertkrimen/otto"
	"time"
)

type ScoreCalculator struct {

}


func (scoreCalculator *ScoreCalculator) CalculateScore(pullRequestWrapper *PullRequestWrapper) PullRequestScore {
	vm := otto.New()
	vm.Run(`
    abc = 2 + 2;
    console.log("The value of abc is " + abc); // 4
`)

	pullRequestScore := PullRequestScore{}

	importantPullRequestReviews := make([]*PullRequestReview, 0)
	for _, pullRequestReviews := range pullRequestWrapper.PullRequest.PullRequestReviewsByUser {
		pullRequestReview := ghm.extractMostImportantFirst(pullRequestReviews)
		importantPullRequestReviews = append(importantPullRequestReviews, pullRequestReview)
	}

	pullRequestScore.NumReviewers = uint(len(importantPullRequestReviews))
	pullRequestScore.Seen = pullRequestWrapper.Seen
	pullRequestScore.AgeSec = uint32(time.Now().Unix() - pullRequestWrapper.FirstSeen.Unix())

	for _, pullRequestReview := range importantPullRequestReviews {
		switch pullRequestReview.Status {
		case "APPROVED" : pullRequestScore.Approvals++
		case "CHANGES_REQUESTED" : pullRequestScore.ChangesRequested++
		case "COMMENTED" : pullRequestScore.Comments++
		}
	}

	return pullRequestScore
}