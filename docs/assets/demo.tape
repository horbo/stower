Output "docs/assets/demo.gif"

Require stow
Require stower

Set Shell "bash"
Set FontFamily "Menlo"
Set FontSize 16
Set Width 1150
Set Height 720
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 70ms

Hide
Type "ROOT=$(./docs/assets/fixture.sh); export HOME=$ROOT/home; cd $HOME; export PS1='$ '; clear"
Enter
Sleep 2s
Show

Type "stower"
Sleep 400ms
Enter
Sleep 3s
Screenshot "docs/assets/overview.png"
Sleep 1s

Type "2"
Sleep 800ms
Enter
Sleep 1200ms
Type "jjjjj"
Sleep 1200ms
Screenshot "docs/assets/home.png"
Sleep 1s

Space
Sleep 1500ms
Type "editors"
Sleep 800ms
Enter
Sleep 2s

Type "3"
Sleep 1500ms
Screenshot "docs/assets/staged.png"
Sleep 1s

Enter
Sleep 1200ms
Type "y"
Sleep 3s
Enter
Sleep 2500ms
Escape
Sleep 800ms

Type "4"
Sleep 1200ms
Type "j"
Sleep 800ms
Screenshot "docs/assets/issues.png"
Sleep 1s
Type "f"
Sleep 3s
Escape
Sleep 1500ms

Type "q"
Sleep 1500ms
