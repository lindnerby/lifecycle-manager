#!/bin/bash

YELLOW="\033[33m"
GREEN="\033[32m"
RESET="\033[0m"

if ! command -v gh &> /dev/null; then
    echo "GitHub CLI (gh) is NOT installed. Please install it, because it is a prerequisite for this script."
    exit 1
else
    echo -e "${GREEN}✔ GitHub CLI (gh) is installed.${RESET}"
    gh --version
fi

while true; do
    echo -e "${YELLOW}What do you want to release? - Enter to default${RESET}"
    echo "1/r) runtime-watcher (default)"
    echo "2/l) lifecycle-manager"
    echo "3/q) Quit/Exit"

    case $choice in
        1)
            echo -e "\e[1mRelease runtime-watcher\e[0m"
            echo "Please enter semantic version for release:"

            exit 0
            ;;
        2)
            echo -e "\e[1mRelease lifecycle-manager\e[0m"
            echo "Please enter semantic version for release:"

           exit 0
            ;;
        3)
            echo "Other option (update sec scanners, ref new runtime-watcher"
            exit 0
            ;;
        *)
            echo "Invalid choice. Please enter 1, 2, or 3."
            ;;
    esac
done


