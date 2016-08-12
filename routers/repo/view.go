// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repo

import (
	"bytes"
	"fmt"
	gotemplate "html/template"
	"io/ioutil"
	"path"
	"strings"

	"github.com/Unknwon/paginater"

	"github.com/gogits/git-module"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/modules/base"
	"github.com/gogits/gogs/modules/context"
	"github.com/gogits/gogs/modules/log"
	"github.com/gogits/gogs/modules/markdown"
	"github.com/gogits/gogs/modules/setting"
	"github.com/gogits/gogs/modules/template"
	"github.com/gogits/gogs/modules/template/highlight"
)

const (
	HOME     base.TplName = "repo/home"
	WATCHERS base.TplName = "repo/watchers"
	FORKS    base.TplName = "repo/forks"
)

func Home(ctx *context.Context) {
	title := ctx.Repo.Repository.Owner.Name + "/" + ctx.Repo.Repository.Name
	if len(ctx.Repo.Repository.Description) > 0 {
		title += ": " + ctx.Repo.Repository.Description
	}
	ctx.Data["Title"] = title
	ctx.Data["PageIsViewCode"] = true
	ctx.Data["RequireHighlightJS"] = true

	branchName := ctx.Repo.BranchName
	userName := ctx.Repo.Owner.Name
	repoName := ctx.Repo.Repository.Name

	repoLink := ctx.Repo.RepoLink
	branchLink := ctx.Repo.RepoLink + "/src/" + branchName
	treeLink := branchLink
	rawLink := ctx.Repo.RepoLink + "/raw/" + branchName

	// Get tree path
	treename := ctx.Repo.TreeName

	if len(treename) > 0 {
		if treename[len(treename)-1] == '/' {
			ctx.Redirect(repoLink + "/src/" + branchName + "/" + treename[:len(treename)-1])
			return
		}

		treeLink += "/" + treename
	}

	treePath := treename
	if len(treePath) != 0 {
		treePath = treePath + "/"
	}

	entry, err := ctx.Repo.Commit.GetTreeEntryByPath(treename)
	if err != nil && git.IsErrNotExist(err) {
		ctx.Handle(404, "GetTreeEntryByPath", err)
		return
	}

	if len(treename) != 0 && entry == nil {
		ctx.Handle(404, "repo.Home", nil)
		return
	}
	if entry != nil && !entry.IsDir() {
		blob := entry.Blob()

		if dataRc, err := blob.Data(); err != nil {
			ctx.Handle(404, "blob.Data", err)
			return
		} else {
			ctx.Data["FileSize"] = blob.Size()
			ctx.Data["IsFile"] = true
			ctx.Data["FileName"] = blob.Name()
			ctx.Data["HighlightClass"] = highlight.FileNameToHighlightClass(blob.Name())
			ctx.Data["FileLink"] = rawLink + "/" + treename

			buf := make([]byte, 1024)
			n, _ := dataRc.Read(buf)
			if n > 0 {
				buf = buf[:n]
			}

			_, isTextFile := base.IsTextFile(buf)
			_, isImageFile := base.IsImageFile(buf)
			_, isPDFFile := base.IsPDFFile(buf)
			ctx.Data["IsFileText"] = isTextFile

			switch {
			case isPDFFile:
				ctx.Data["IsPDFFile"] = true
			case isImageFile:
				ctx.Data["IsImageFile"] = true
			case isTextFile:
				if blob.Size() >= setting.UI.MaxDisplayFileSize {
					ctx.Data["IsFileTooLarge"] = true
				} else {
					ctx.Data["IsFileTooLarge"] = false
					d, _ := ioutil.ReadAll(dataRc)
					buf = append(buf, d...)
					readmeExist := markdown.IsMarkdownFile(blob.Name()) || markdown.IsReadmeFile(blob.Name())
					ctx.Data["ReadmeExist"] = readmeExist
					if readmeExist {
						ctx.Data["FileContent"] = string(markdown.Render(buf, path.Dir(treeLink), ctx.Repo.Repository.ComposeMetas()))
					} else {
						// Building code view blocks with line number on server side.
						var filecontent string
						if err, content := template.ToUTF8WithErr(buf); err != nil {
							if err != nil {
								log.Error(4, "ToUTF8WithErr: %s", err)
							}
							filecontent = string(buf)
						} else {
							filecontent = content
						}

						var output bytes.Buffer
						lines := strings.Split(filecontent, "\n")
						for index, line := range lines {
							output.WriteString(fmt.Sprintf(`<li class="L%d" rel="L%d">%s</li>`, index+1, index+1, gotemplate.HTMLEscapeString(line)) + "\n")
						}
						ctx.Data["FileContent"] = gotemplate.HTML(output.String())

						output.Reset()
						for i := 0; i < len(lines); i++ {
							output.WriteString(fmt.Sprintf(`<span id="L%d">%d</span>`, i+1, i+1))
						}
						ctx.Data["LineNums"] = gotemplate.HTML(output.String())
					}
				}
			}
		}
	} else {
		// Directory and file list.
		tree, err := ctx.Repo.Commit.SubTree(treename)
		if err != nil {
			ctx.Handle(404, "SubTree", err)
			return
		}

		entries, err := tree.ListEntries()
		if err != nil {
			ctx.Handle(500, "ListEntries", err)
			return
		}
		entries.Sort()

		ctx.Data["Files"], err = entries.GetCommitsInfo(ctx.Repo.Commit, treePath)
		if err != nil {
			ctx.Handle(500, "GetCommitsInfo", err)
			return
		}

		var readmeFile *git.Blob
		for _, f := range entries {
			if f.IsDir() || !markdown.IsReadmeFile(f.Name()) {
				continue
			} else {
				readmeFile = f.Blob()
				break
			}
		}

		if readmeFile != nil {
			ctx.Data["ReadmeInList"] = true
			ctx.Data["ReadmeExist"] = true
			if dataRc, err := readmeFile.Data(); err != nil {
				ctx.Handle(404, "repo.SinglereadmeFile.Data", err)
				return
			} else {

				buf := make([]byte, 1024)
				n, _ := dataRc.Read(buf)
				if n > 0 {
					buf = buf[:n]
				}

				ctx.Data["FileSize"] = readmeFile.Size()
				ctx.Data["FileLink"] = rawLink + "/" + treename
				_, isTextFile := base.IsTextFile(buf)
				ctx.Data["FileIsText"] = isTextFile
				ctx.Data["FileName"] = readmeFile.Name()
				if isTextFile {
					d, _ := ioutil.ReadAll(dataRc)
					buf = append(buf, d...)
					switch {
					case markdown.IsMarkdownFile(readmeFile.Name()):
						buf = markdown.Render(buf, treeLink, ctx.Repo.Repository.ComposeMetas())
					default:
						buf = bytes.Replace(buf, []byte("\n"), []byte(`<br>`), -1)
					}
					ctx.Data["FileContent"] = string(buf)
				}
			}
		}

		lastCommit := ctx.Repo.Commit
		if len(treePath) > 0 {
			c, err := ctx.Repo.Commit.GetCommitByPath(treePath)
			if err != nil {
				ctx.Handle(500, "GetCommitByPath", err)
				return
			}
			lastCommit = c
		}
		ctx.Data["LastCommit"] = lastCommit
		ctx.Data["LastCommitUser"] = models.ValidateCommitWithEmail(lastCommit)
	}

	ctx.Data["Username"] = userName
	ctx.Data["Reponame"] = repoName

	var treenames []string
	paths := make([]string, 0)

	if len(treename) > 0 {
		treenames = strings.Split(treename, "/")
		for i := range treenames {
			paths = append(paths, strings.Join(treenames[0:i+1], "/"))
		}

		ctx.Data["HasParentPath"] = true
		if len(paths)-2 >= 0 {
			ctx.Data["ParentPath"] = "/" + paths[len(paths)-2]
		}
	}

	ctx.Data["Paths"] = paths
	ctx.Data["TreeName"] = treename
	ctx.Data["Treenames"] = treenames
	ctx.Data["TreePath"] = treePath
	ctx.Data["BranchLink"] = branchLink
	ctx.HTML(200, HOME)
}

func RenderUserCards(ctx *context.Context, total int, getter func(page int) ([]*models.User, error), tpl base.TplName) {
	page := ctx.QueryInt("page")
	if page <= 0 {
		page = 1
	}
	pager := paginater.New(total, models.ItemsPerPage, page, 5)
	ctx.Data["Page"] = pager

	items, err := getter(pager.Current())
	if err != nil {
		ctx.Handle(500, "getter", err)
		return
	}
	ctx.Data["Cards"] = items

	ctx.HTML(200, tpl)
}

func Watchers(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.watchers")
	ctx.Data["CardsTitle"] = ctx.Tr("repo.watchers")
	ctx.Data["PageIsWatchers"] = true
	RenderUserCards(ctx, ctx.Repo.Repository.NumWatches, ctx.Repo.Repository.GetWatchers, WATCHERS)
}

func Stars(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.stargazers")
	ctx.Data["CardsTitle"] = ctx.Tr("repo.stargazers")
	ctx.Data["PageIsStargazers"] = true
	RenderUserCards(ctx, ctx.Repo.Repository.NumStars, ctx.Repo.Repository.GetStargazers, WATCHERS)
}

func Forks(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repos.forks")

	forks, err := ctx.Repo.Repository.GetForks()
	if err != nil {
		ctx.Handle(500, "GetForks", err)
		return
	}

	for _, fork := range forks {
		if err = fork.GetOwner(); err != nil {
			ctx.Handle(500, "GetOwner", err)
			return
		}
	}
	ctx.Data["Forks"] = forks

	ctx.HTML(200, FORKS)
}
