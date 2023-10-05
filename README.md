# Elections

## Setting up the CSSS election system:

1. SSH into any student server, create two new directories in the home directory with `mkdir public_html` and `mkdir csss`
2. Modify the permission of `public_html` to be permissions 711 (use `ls -l` to check current permissions and `chmod 711 <directory/file name>` to change permissions)
3. `cd public_html` and fetch the repo content directly with the following command. The idea is that everything in this repo should be in `~/public_html`
    ```
    git init
    git remote add origin https://github.com/ubccsss/elections.git
    git fetch origin
    git checkout -b master --track origin/master
    ```
4.  Make sure all files in `public_html` have 644 permissions, particularly the `.htaccess` file, `index.html`, and `style.css`. Any scripts may have even 700 permissions as they run under suexec (as of September 2019) by the department's deisgn. (Don't even bother trying to run apache locally.) The `images/` folder may need to have 711 permissions, and ensure the individual images have 644 or 744 permissions as well.
5. Run `make` to generate the `elections.cgi` file
6. `touch elections.db` (or whatever you want to call the dbfile)
7. Open another terminal, `cd ~/csss`, generate a PKCS1 private key with `openssl genrsa -traditional -out private.pem 2048` and give it 600 permissions (making sure the `~/.ssh` and `~/csss` folders are also accessible, with 700 permissions)
8. Go back to the `~/public_html`, modify the `config.yml` to your liking. Note that the absolute path is something like `/home/<letter>/<cwl>/`
9. Bootstrap the database: run `./elections.cgi -migrate`. At this stage, you should be able to open your browser and see the election website at `https://www.students.cs.ubc.ca/~YOUR_CWL/index.html`
10. In your other teminal for `~/csss`, create `sids.txt` and fill it in with information you get from Giuliana or whichever admin from the CS department is in charge 
11.  Test. If something fails, erase, re-bootstrap the elections.db and run `./elections.cgi -migrate` again.

## Updating candidates
Each candidate should submit a bio (historically, the limit has been 200 words) and a headshot.

To update candidate bios, edit the description under the `bios` key in `config.yml`. Take care to indent the bios properly if they are multiline.

To copy over the headshots from your local machine, use `scp ./local_path yourcwl@remote.students.cs.ubc.ca/public_html/images/candidate_name.jpg`. See [the SCP man page](https://linux.die.net/man/1/scp) for more information. Then, update the paths in `config.yml`.

If certain positions are not being voted on, you can delete them from the `positions` key in `config.yml`. Otherwise, under `positions.candidates`, list the candidates for that position. The values provided must exactly match the names of candidates in the `bios` section.

## Updating template, style, scripts
If there are stylistic/structural changes to our main website, you may want to sync those changes here in this repo. The way to do it is simply by running `go run gettemplate/gettemplate.go` in the root folder. Note that this currently `gettemplate.go` is outdated so you will have to manually change a couple things in the new `template.html` file. This includes:
1. Make sure that the html between the header and footer tags is "empty". See previous git commits for `template.html` for examples
2. Get rid of the integrity property in the style.css link tag (there might be a better solution?)
3. Fix broken images by adding the `https://ubccsss.org` prefix

## References
- https://my.cs.ubc.ca/docs/setting-personal-website
