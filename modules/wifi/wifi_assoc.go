package wifi

import (
	"bytes"
	"fmt"
	"net"
	"sort"

	"github.com/bettercap/bettercap/network"
	"github.com/bettercap/bettercap/packets"
)

func (mod *WiFiModule) sendAssocPacket(ap *network.AccessPoint) {
	if err, pkt := packets.NewDot11Auth(mod.Session.Interface.HW, ap.HW, 1); err != nil {
		mod.Error("cloud not create auth packet: %s", err)
	} else {
		mod.injectPacket(pkt)
	}

	if err, pkt := packets.NewDot11AssociationRequest(mod.Session.Interface.HW, ap.HW, ap.ESSID(), 1); err != nil {
		mod.Error("cloud not create association request packet: %s", err)
	} else {
		mod.injectPacket(pkt)
	}
}

func (mod *WiFiModule) skipAssoc(to net.HardwareAddr) bool {
	for _, mac := range mod.assocSkip {
		if bytes.Equal(to, mac) {
			return true
		}
	}
	return false
}

func (mod *WiFiModule) isAssocSilent() bool {
	if err, is := mod.BoolParam("wifi.assoc.silent"); err != nil {
		mod.Warning("%v", err)
	} else {
		mod.assocSilent = is
	}
	return mod.assocSilent
}

func (mod *WiFiModule) doAssocOpen() bool {
	if err, is := mod.BoolParam("wifi.assoc.open"); err != nil {
		mod.Warning("%v", err)
	} else {
		mod.assocOpen = is
	}
	return mod.assocOpen
}

func (mod *WiFiModule) startAssoc(to net.HardwareAddr) error {
	// parse skip list
	if err, assocSkip := mod.StringParam("wifi.assoc.skip"); err != nil {
		return err
	} else if macs, err := network.ParseMACs(assocSkip); err != nil {
		return err
	} else {
		mod.assocSkip = macs
	}

	// if not already running, temporarily enable the pcap handle
	// for packet injection
	if !mod.Running() {
		if err := mod.Configure(); err != nil {
			return err
		}
		defer mod.handle.Close()
	}

	toAssoc := make([]*network.AccessPoint, 0)
	isBcast := network.IsBroadcastMac(to)
	for _, ap := range mod.Session.WiFi.List() {
		if isBcast || bytes.Equal(ap.HW, to) {
			if !mod.skipAssoc(ap.HW) {
				toAssoc = append(toAssoc, ap)
			} else {
				mod.Debug("skipping ap:%v because skip list %v", ap, mod.assocSkip)
			}
		}
	}

	if len(toAssoc) == 0 {
		if isBcast {
			return nil
		}
		return fmt.Errorf("%s is an unknown BSSID or it is in the association skip list.", to.String())
	}

	go func() {
		mod.writes.Add(1)
		defer mod.writes.Done()

		// since we need to change the wifi adapter channel for each
		// association request, let's sort by channel so we do the minimum
		// amount of hops possible
		sort.Slice(toAssoc, func(i, j int) bool {
			return toAssoc[i].Channel < toAssoc[j].Channel
		})

		// send the association request frames
		for _, ap := range toAssoc {
			if mod.Running() {
				logger := mod.Info
				if mod.isAssocSilent() {
					logger = mod.Debug
				}

				if ap.IsOpen() && !mod.doAssocOpen() {
					mod.Debug("skipping association for open network %s (wifi.assoc.open is false)", ap.ESSID())
				} else {
					logger("sending association request to AP %s (channel:%d encryption:%s)", ap.ESSID(), ap.Channel, ap.Encryption)

					mod.onChannel(ap.Channel, func() {
						mod.sendAssocPacket(ap)
					})
				}
			}
		}
	}()

	return nil
}
