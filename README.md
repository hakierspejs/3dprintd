# 3dprintd

This project was created because in Hakierspejs, we have multiple printers whose Octoprint is only accessible from within the hackerspace's network.

Because of that, one of the members decided to build a light, limited and public web frontend that would allow to monitor printing process and abort it if things go wrong - straight from one's mobile phone, with minimal authentication (no VPN etc.).

There's also a monitor routine built in that sends notifications when the state of monitored printers changes. Messages are sent on a channel dedicated to discussions about our 3D printers.

Here's a preview of the monitoring view:

![obraz](https://github.com/user-attachments/assets/6ed0c790-bce8-422d-b4df-d8331a395e28)
