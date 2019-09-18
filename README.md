# elections

## Get this working:

1. Run make and generate .cgi file
2. Go to your space on the computer science server and create a public_html folder in the home directory with permissions 755 (don't even bother trying to run apache locally). Add all files, making sure they have 644 permissions and folders have 755 permissions. Any scripts may have even 700 permissions as they run under suexec (as of September 2019) by the department's deisgn
3. > touch dbfile.db
(or whatever you want to call the dbfile)
4. Generate a PKCS1 private key and give it 600 permissions (making sure the .ssh folder is also accessible)
> openssl genrsa -out private.pem 2048 
5. Modify the config.yml to your liking. Note that the absolute path is something like /home/<letter>/<cwl>/
6. Bootstrap the database: run > ./elections -migrate
7. Update the sids.txt with information you get from Giuliana or whichever admin is in charge 
8. Test. If something fails, erase, re-bootstrap the dbfile.db and run ./elections -migrate again.
