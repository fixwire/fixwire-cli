# Contributing

Thanks for helping. Issues and pull requests are welcome here.

The tool is developed together with the Fixwire server, which keeps it in
step with the API it calls; this repository is updated from there, with its
full history. We apply an accepted pull request there, with you as its
author, and it comes back here with the next update.

1. **Open an issue first** for anything larger than a fix, so we can agree
   on the approach before you spend time on it.
2. **Keep pull requests small**, one change each, with tests.
3. **Run the checks** before you push:

   ```sh
   go vet ./... && go test -race ./...
   golangci-lint run ./...
   node --test 'npm/test/*.test.mjs'   # the npm package's launcher and packing
   ```

4. **The API key stays secret.** It comes from a flag or the environment and
   never appears in output, usage text included; tests check this.

Commit messages say what changed and why, in plain words.
