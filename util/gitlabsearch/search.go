package gitlabsearch

import (
	"fmt"
	"github.com/neal1991/gshark/logger"
	"github.com/neal1991/gshark/models"
	"github.com/neal1991/gshark/vars"
	"github.com/xanzy/go-gitlab"
	"sync"
	"time"
)

func RunTask(duration time.Duration) {
	RunSearchTask(GenerateSearchCodeTask())

	logger.Log.Infof("Complete the scan of Github, start to sleep %v seconds", duration*time.Second)
	time.Sleep(duration * time.Second)
}

func GenerateSearchCodeTask() (map[int][]models.Rule, error) {
	result := make(map[int][]models.Rule)
	// get rules with the type of github
	rules, err := models.GetValidRulesByType(vars.GITLAB)
	ruleNum := len(rules)
	batch := ruleNum / vars.SearchNum

	for i := 0; i < batch; i++ {
		result[i] = rules[vars.SearchNum*i : vars.SearchNum*(i+1)]
	}

	if ruleNum%vars.SearchNum != 0 {
		result[batch] = rules[vars.SearchNum*batch : ruleNum]
	}
	return result, err
}

func RunSearchTask(mapRules map[int][]models.Rule, err error) {
	client := GetClient()
	// get all public projects
	GetProjects(client)
	if err == nil {
		for _, rules := range mapRules {
			startTime := time.Now()
			Search(rules, client)
			usedTime := time.Since(startTime).Seconds()
			if usedTime < 60 {
				time.Sleep(time.Duration(60 - usedTime))
			}
		}
	}
}

func Search(rules []models.Rule, client *gitlab.Client) {
	var wg sync.WaitGroup
	wg.Add(len(rules))

	for _, rule := range rules {
		go func(rule models.Rule) {
			defer wg.Done()
			SearchInsideProjects(rule.Pattern, client)
		}(rule)
	}
	wg.Wait()
}

func SearchInsideProjects(keyword string, client *gitlab.Client) {
	projects := ListValidProjects()
	for _, project := range projects {
		results := SearchCode(keyword, project, client)
		SaveResult(results, &keyword)
	}
}

func SaveResult(results []*models.CodeResult, keyword *string) {
	insertCount := 0
	if len(results) > 0 {
		for _, resultItem := range results {
			has, err := resultItem.Exist()
			if err != nil {
				logger.Log.Error(err)
			}
			if !has {
				resultItem.Keyword = keyword
				resultItem.Insert()
				insertCount++
			}

		}
		logger.Log.Infof("Has inserted %d results into code_result", insertCount)
	}
}

func SearchCode(keyword string, project models.InputInfo, client *gitlab.Client) []*models.CodeResult {
	codeResults := make([]*models.CodeResult, 0)
	results, resp, err := client.Search.BlobsByProject(project.ProjectId, keyword, &gitlab.SearchOptions{})
	if err != nil {
		fmt.Println(err)
	}
	if resp.StatusCode != 200 {
		fmt.Printf("request error: %d", resp.StatusCode)
		return codeResults
	}
	for _, result := range results {
		url := project.Url + result.Basename + result.Filename
		textMatches := make([]models.TextMatch, 1)
		textMatch := models.TextMatch{
			Fragment: &result.Data,
		}
		textMatches = append(textMatches, textMatch)
		codeResult := models.CodeResult{
			Id:          0,
			Name:        &result.Filename,
			Path:        &result.Basename,
			RepoName:    result.Basename,
			HTMLURL:     &url,
			TextMatches: textMatches,
			Status:      0,
			Keyword:     &keyword,
			Source:      vars.Source,
		}
		codeResults = append(codeResults, &codeResult)
		err := models.UpdateStatusById(1, result.ProjectID)
		if err != nil {
			logger.Log.Error(err)
		}
	}
	return codeResults
}

func ListValidProjects() []models.InputInfo {
	validProjects := make([]models.InputInfo, 0)
	projects, err := models.ListInputInfoByType(vars.GITLAB)
	if err != nil {
		logger.Log.Error(err)
	}
	for _, p := range projects {
		// if the project has been searched
		if p.Status == 1 {
			continue
		}
		validProjects = append(validProjects, p)
	}
	return validProjects
}

func GetClient() *gitlab.Client {
	tokens, err := models.ListValidTokens(vars.GITLAB)
	if err != nil {
		logger.Log.Error(err)
	}
	return gitlab.NewClient(nil, tokens[0].Token)
}

// GetProjects is utilized to obtain public projects from gitlab
func GetProjects(client *gitlab.Client) {
	opt := &gitlab.ListProjectsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}
	for {
		// Get the first page with projects.
		ps, resp, err := client.Projects.ListProjects(opt)
		if err != nil {
			fmt.Println(err)
		}

		// List all the projects we've found so far.
		for _, p := range ps {
			inputInfo := models.InputInfo{
				Url:       p.WebURL,
				Path:      p.PathWithNamespace,
				Type:      vars.GITLAB,
				ProjectId: p.ID,
			}
			has, err := inputInfo.Exist()
			if err != nil {
				fmt.Println(err)
			}
			if !has {
				inputInfo.Insert()
			}
		}

		if resp.NextPage == 0 {
			fmt.Println("next page is 0")
			break
		}

		if resp.StatusCode != 200 {
			fmt.Printf("request error: %d", resp.StatusCode)
			break
		}

		opt.Page = resp.NextPage
	}
}