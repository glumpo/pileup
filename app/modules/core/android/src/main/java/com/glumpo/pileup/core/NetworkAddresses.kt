package com.glumpo.pileup.core

import android.net.ConnectivityManager
import android.net.NetworkCapabilities

class NetworkAddresses(private val connectivity: ConnectivityManager) : mobile.NetworkInfo {
  override fun addrs(): String =
    jsonStrings(
      connectivity.allNetworks
        .filter { connectivity.getNetworkCapabilities(it)?.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) == false }
        .flatMap { connectivity.getLinkProperties(it)?.linkAddresses.orEmpty() }
        .map { it.address }
        .filter { !it.isLoopbackAddress }
        .mapNotNull { it.hostAddress }
    )
}
