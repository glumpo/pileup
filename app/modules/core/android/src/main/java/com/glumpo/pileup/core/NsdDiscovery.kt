package com.glumpo.pileup.core

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.net.wifi.WifiManager
import mobile.Mobile

class NsdDiscovery(context: Context) : mobile.DiscoveryProvider {
  private val nsd = context.getSystemService(NsdManager::class.java)
  private val multicast = context.getSystemService(WifiManager::class.java)
    .createMulticastLock("pileup-discovery")
    .apply { setReferenceCounted(false) }
  private val session = Session(multicast::acquire, multicast::release)
  private val resolves = SerialQueue<Pair<NsdServiceInfo, mobile.DiscoveryHandler>> { (info, handler), done ->
    resolve(info, handler, done)
  }

  override fun advertise(service: String, name: String, port: Long, txt: ByteArray) {
    val info = NsdServiceInfo().apply {
      serviceType = service
      serviceName = name
      setPort(port.toInt())
      txtAttributes(txt).forEach { (key, value) -> setAttribute(key, value) }
    }
    val listener = object : NsdManager.RegistrationListener {
      override fun onServiceRegistered(info: NsdServiceInfo) {}
      override fun onRegistrationFailed(info: NsdServiceInfo, error: Int) = failed("advertise $service", error)
      override fun onServiceUnregistered(info: NsdServiceInfo) {}
      override fun onUnregistrationFailed(info: NsdServiceInfo, error: Int) = failed("stop advertising $service", error)
    }
    session.begin { nsd.unregisterService(listener) }
    nsd.registerService(info, NsdManager.PROTOCOL_DNS_SD, listener)
  }

  override fun browse(service: String, handler: mobile.DiscoveryHandler) {
    val listener = object : NsdManager.DiscoveryListener {
      override fun onDiscoveryStarted(type: String) {}
      override fun onDiscoveryStopped(type: String) {}
      override fun onStartDiscoveryFailed(type: String, error: Int) = failed("browse $service", error)
      override fun onStopDiscoveryFailed(type: String, error: Int) = failed("stop browsing $service", error)
      override fun onServiceFound(info: NsdServiceInfo) = resolves.add(info to handler)
      override fun onServiceLost(info: NsdServiceInfo) = handler.lost(info.serviceName)
    }
    session.begin { nsd.stopServiceDiscovery(listener) }
    nsd.discoverServices(service, NsdManager.PROTOCOL_DNS_SD, listener)
  }

  override fun stop() {
    resolves.clear()
    session.end()
  }

  private fun failed(what: String, error: Int) = Mobile.report("discovery", "nsd: $what failed, error $error")

  private fun resolve(found: NsdServiceInfo, handler: mobile.DiscoveryHandler, done: () -> Unit) {
    val listener = object : NsdManager.ResolveListener {
      override fun onResolveFailed(info: NsdServiceInfo, error: Int) {
        failed("resolve ${found.serviceType}", error)
        done()
      }
      override fun onServiceResolved(info: NsdServiceInfo) {
        try {
          info.host?.hostAddress?.let { handler.found(found.serviceName, it, info.port.toLong(), txtRecord(info.attributes)) }
        } finally {
          done()
        }
      }
    }
    try {
      nsd.resolveService(found, listener)
    } catch (e: IllegalArgumentException) {
      Mobile.report("discovery", "nsd: resolve ${found.serviceType}: $e")
      done()
    }
  }
}
