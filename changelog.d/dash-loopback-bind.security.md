**`heavyq dash --addr localhost:PORT` always listens on loopback.** `localhost` used to go through the hosts file
and DNS, so a broken or hostile resolver could put the dashboard on a LAN address. It now means `127.0.0.1` and is
never looked up, and any listener that is not bound to a loopback address is closed and refused.
