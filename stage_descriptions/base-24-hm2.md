In this stage, you'll make a release a Git commit that Argo CD syncs and a revert undoes.

Running `helm upgrade` by hand means nobody can tell what runs in nonprod without asking the cluster. With GitOps the answer is in Git: the pipeline commits the new image tag to `values-nonprod.yaml`, and Argo CD makes the cluster match. Rolling back is `git revert`.

### New here?

- **GitOps**: what runs in the cluster is whatever Git says. A tool watches the repository and makes the cluster match it.
- **Argo CD**: that tool. An **Application** object tells it which repository path and values file to sync, and where.
- **PreSync hook**: a Job Argo CD runs before it changes anything else, here the migration.
- **`[skip ci]`**: a marker in a commit message that tells GitLab not to start a pipeline for that commit.

### Expected behaviour

- **Stages:** `.gitlab-ci.yml` has `stages:` with `test`, `build` and `deploy`, in that order. Each of `build` and `deploy` holds exactly one job, and both run only on the default branch (`rules: - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH`).
- **The build job:** it pushes an image tagged with the commit (`$CI_COMMIT_SHORT_SHA`).
- **The deploy job:**
  - it has `needs: [<the build job>]`;
  - it sets `image.tag` in `deploy/chart/values-nonprod.yaml` to `$CI_COMMIT_SHORT_SHA`, and changes no other file;
  - it commits that one change with `[skip ci]` in the message and pushes it to the default branch, at `https://<user>:${DEPLOY_TOKEN}@${CI_SERVER_HOST}/${CI_PROJECT_PATH}.git`.
- **The Argo CD Application:** `deploy/argocd-application.yaml` holds the one object you hand devops. Its source is `path: deploy/chart` with `helm.valueFiles: [values-nonprod.yaml]`, and its `syncPolicy.automated.selfHeal` is `true`.
- **Migrations:** the migrate Job carries `argocd.argoproj.io/hook: PreSync`, so the schema changes before new pods start.

### Tests

The tester checks `.gitlab-ci.yml`, the Application, and the PreSync annotation in the rendered chart. Then it plays GitLab. It makes a bare repository holding your code, with one commit on `main`, and clones it the way a runner does: a detached HEAD at that commit. Your deploy job's script then runs with `sh` on this machine, so it needs `git`. Its `variables:` apply, and so do `CI_COMMIT_SHA`, `CI_COMMIT_SHORT_SHA`, `CI_DEFAULT_BRANCH=main`, `CI_SERVER_HOST`, `CI_PROJECT_PATH`, `CI_REPOSITORY_URL` and `DEPLOY_TOKEN`. Pushes to the project's URL go to the bare repository.

Afterwards:

- `main` must hold exactly one new commit;
- its message contains `[skip ci]`;
- it changes only `deploy/chart/values-nonprod.yaml`;
- the chart rendered from it runs image tag `$CI_COMMIT_SHORT_SHA`.

Finally, `git revert` of that commit must bring back the old tag.

The build job isn't run: pushing to Quay, and Argo CD's sync, happen in the assignment, with devops.

### Why your design just broke

Up to now, deploying meant someone running a command. That leaves no record of who changed what, and it's undone by another command nobody remembers. When the release is a commit, history becomes the deploy log, review covers releases too, and the cluster heals back to Git when someone edits it by hand. One trap: a pipeline that commits starts a pipeline, and that one commits again. `[skip ci]` breaks the loop. The order matters too: a migration that runs after new pods start serves new code against the old schema.

### Pointers

- [OpenGitOps principles](https://opengitops.dev/).
- Argo CD: [Helm](https://argo-cd.readthedocs.io/en/stable/user-guide/helm/), [Automated Sync Policy](https://argo-cd.readthedocs.io/en/stable/user-guide/auto_sync/) and [Sync Phases and Waves](https://argo-cd.readthedocs.io/en/stable/user-guide/sync-waves/).
- GitLab: [skip a pipeline](https://docs.gitlab.com/ci/pipelines/#skip-a-pipeline), [project access tokens](https://docs.gitlab.com/user/project/settings/project_access_tokens/), [predefined variables](https://docs.gitlab.com/ci/variables/predefined_variables/).
- Search: `gitops image tag bump pipeline`, `argocd presync hook database migration`.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Releasing becomes a commit. After the image is built and pushed, a deploy job edits one line, `image.tag` in `values-nonprod.yaml`, commits it with `[skip ci]` and pushes it to the default branch. Argo CD notices and syncs. Rolling back is reverting that commit.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

The deploy job's script, in outline:

```sh
sed -i "s/^  tag:.*/  tag: \"$CI_COMMIT_SHORT_SHA\"/" deploy/chart/values-nonprod.yaml
git config user.name "Framewise CI" && git config user.email "ci@framewise.invalid"
git commit -am "Deploy $CI_COMMIT_SHORT_SHA to nonprod [skip ci]"
git push "https://deploy:${DEPLOY_TOKEN}@${CI_SERVER_HOST}/${CI_PROJECT_PATH}.git" HEAD:"$CI_DEFAULT_BRANCH"
```

`deploy/argocd-application.yaml` is one `argoproj.io/v1alpha1` `Application` with `source.path: deploy/chart`, `source.helm.valueFiles: [values-nonprod.yaml]` and `syncPolicy.automated.selfHeal: true`. The migrate Job gets the annotation `argocd.argoproj.io/hook: PreSync`.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

A runner checks out a detached HEAD, so there is no current branch to push: push `HEAD:$CI_DEFAULT_BRANCH`. In `.gitlab-ci.yml`, quote a script line holding `: ` or YAML reads it as a map. Make the `sed` touch only the `image.tag` line (look at the diff). Without `[skip ci]`, the deploy commit starts a pipeline that deploys again, forever.

</details>
