(function () {
  const root = document.querySelector("[data-analytics-root]");
  if (!root) {
    return;
  }

  const endpoint = "/dashboard/analytics/data";
  const pollIntervalMs = 1000;
  const refreshButton = root.querySelector("[data-analytics-refresh]");

  function escapeHTML(value) {
    return String(value)
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#39;");
  }

  function formatCount(value) {
    const numeric = Number(value || 0);
    return new Intl.NumberFormat().format(numeric);
  }

  function setText(id, value) {
    const node = document.getElementById(id);
    if (!node) {
      return;
    }
    node.textContent = value;
  }

  function renderRPM(points) {
    const node = document.getElementById("analytics-rpm-chart");
    if (!node) {
      return;
    }
    if (!Array.isArray(points) || points.length === 0) {
      node.innerHTML = '<p class="empty-copy">No request data yet.</p>';
      setText("analytics-rpm-current", "0 this minute");
      return;
    }

    const windowPoints = points.slice(-30);
    const counts = windowPoints.map((point) => Number(point.count || 0));
    const maxCount = Math.max(...counts, 1);
    const latest = counts[counts.length - 1] || 0;
    const svgWidth = 720;
    const svgHeight = 196;
    const paddingX = 20;
    const paddingTop = 40;
    const paddingBottom = 26;
    const graphHeight = svgHeight - paddingTop - paddingBottom;
    const chartWidth = svgWidth - paddingX * 2;
    const denominator = Math.max(windowPoints.length - 1, 1);

    const pointData = windowPoints.map((point, idx) => {
      const count = counts[idx];
      const minute = new Date(point.minute);
      const label = Number.isNaN(minute.getTime())
        ? `${count}`
        : `${minute.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}: ${count}`;
      const x = paddingX + (chartWidth * idx) / denominator;
      const y = paddingTop + graphHeight - (count / maxCount) * graphHeight;
      return {
        x,
        y,
        count,
        label,
        tick: Number.isNaN(minute.getTime())
          ? ""
          : minute.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
      };
    });

    const linePath = pointData
      .map((point, idx) => (idx === 0 ? "M" : "L") + point.x.toFixed(1) + " " + point.y.toFixed(1))
      .join(" ");
    const areaPath =
      linePath +
      " L " +
      pointData[pointData.length - 1].x.toFixed(1) +
      " " +
      (svgHeight - paddingBottom).toFixed(1) +
      " L " +
      pointData[0].x.toFixed(1) +
      " " +
      (svgHeight - paddingBottom).toFixed(1) +
      " Z";
    const tickIndexes = [0, Math.floor((pointData.length - 1) / 2), pointData.length - 1].filter(
      (value, index, arr) => arr.indexOf(value) === index
    );
    const latestPoint = pointData[pointData.length - 1];

    node.innerHTML =
      '<svg class="rpm-chart-svg" viewBox="0 0 ' +
      svgWidth +
      " " +
      svgHeight +
      '" role="img" aria-label="Requests per minute line chart">' +
      '<defs>' +
      '<linearGradient id="rpmAreaGradient" x1="0" x2="0" y1="0" y2="1">' +
      '<stop offset="0%" stop-color="rgba(103,193,221,0.38)"></stop>' +
      '<stop offset="100%" stop-color="rgba(36,83,166,0.04)"></stop>' +
      "</linearGradient>" +
      '<linearGradient id="rpmLineGradient" x1="0" x2="1" y1="0" y2="0">' +
      '<stop offset="0%" stop-color="#67c1dd"></stop>' +
      '<stop offset="100%" stop-color="#2453a6"></stop>' +
      "</linearGradient>" +
      "</defs>" +
      '<line class="rpm-grid-line" x1="' +
      paddingX +
      '" y1="' +
      (svgHeight - paddingBottom) +
      '" x2="' +
      (svgWidth - paddingX) +
      '" y2="' +
      (svgHeight - paddingBottom) +
      '"></line>' +
      '<path class="rpm-area" d="' +
      areaPath +
      '"></path>' +
      '<path class="rpm-line" d="' +
      linePath +
      '"></path>' +
      pointData
        .map((point) => {
          return (
            '<g class="rpm-point-group">' +
            '<circle class="rpm-point-halo" cx="' +
            point.x.toFixed(1) +
            '" cy="' +
            point.y.toFixed(1) +
            '" r="7"></circle>' +
            '<circle class="rpm-point" cx="' +
            point.x.toFixed(1) +
            '" cy="' +
            point.y.toFixed(1) +
            '" r="3.5"></circle>' +
            "<title>" +
            escapeHTML(point.label) +
            "</title>" +
            "</g>"
          );
        })
        .join("") +
      tickIndexes
        .map((idx) => {
          const point = pointData[idx];
          return (
            '<text class="rpm-axis-label" x="' +
            point.x.toFixed(1) +
            '" y="' +
            (svgHeight - 8) +
            '">' +
            escapeHTML(point.tick) +
            "</text>"
          );
        })
        .join("") +
      '<text class="rpm-max-label" x="' +
      (svgWidth - paddingX) +
      '" y="' +
      18 +
      '">' +
      escapeHTML("Peak " + formatCount(maxCount) + " req/min") +
      "</text>" +
      '<circle class="rpm-latest-point" cx="' +
      latestPoint.x.toFixed(1) +
      '" cy="' +
      latestPoint.y.toFixed(1) +
      '" r="5"></circle>' +
      "</svg>";

    setText("analytics-rpm-current", formatCount(latest) + " this minute");
  }

  function renderTenantList(containerID, tenants, key, emptyMessage) {
    const node = document.getElementById(containerID);
    if (!node) {
      return;
    }
    if (!Array.isArray(tenants) || tenants.length === 0) {
      node.innerHTML = '<p class="empty-copy">' + escapeHTML(emptyMessage) + "</p>";
      return;
    }
    node.innerHTML = tenants
      .map((tenant) => {
        const tenantID = escapeHTML(tenant.tenant_id || "unknown");
        const value = formatCount(tenant[key] || 0);
        return (
          '<div class="stack-row">' +
          "<div>" +
          '<p class="stack-title">' +
          tenantID +
          "</p>" +
          "</div>" +
          '<p class="stack-value">' +
          value +
          "</p>" +
          "</div>"
        );
      })
      .join("");
  }

  function renderEvents(events) {
    const node = document.getElementById("analytics-recent-events");
    if (!node) {
      return;
    }
    if (!Array.isArray(events) || events.length === 0) {
      node.innerHTML = '<p class="empty-copy">No recent events.</p>';
      return;
    }
    node.innerHTML = events
      .map((event) => {
        const eventTime = new Date(event.at);
        const formattedTime = Number.isNaN(eventTime.getTime())
          ? "now"
          : eventTime.toLocaleTimeString();
        const method = event.method ? String(event.method).toUpperCase() : event.type;
        const path = event.path || "";
        const tenant = event.tenant_id ? " tenant " + event.tenant_id : "";
        const detail = event.detail ? " " + event.detail : "";
        const summary =
          formattedTime +
          " " +
          method +
          " " +
          path +
          tenant +
          " status " +
          String(event.status || 0) +
          " " +
          String(event.latency_ms || 0) +
          "ms" +
          detail;
        return (
          '<div class="stack-row analytics-event-row">' +
          '<p class="stack-title">' +
          escapeHTML(summary) +
          "</p>" +
          "</div>"
        );
      })
      .join("");
  }

  function render(snapshot) {
    setText("analytics-active-requests", formatCount(snapshot.active_requests));
    setText("analytics-store-count", formatCount(snapshot.store_count));
    setText("analytics-batch-store-count", formatCount(snapshot.batch_store_count));
    setText("analytics-search-count", formatCount(snapshot.search_count));

    renderRPM(snapshot.requests_per_minute || []);
    renderTenantList("analytics-top-writes", snapshot.top_tenants_by_writes || [], "writes", "No write activity yet.");
    renderTenantList("analytics-top-searches", snapshot.top_tenants_by_searches || [], "searches", "No search activity yet.");
    renderEvents(snapshot.recent_events || []);

    if (snapshot.generated_at) {
      const node = document.getElementById("analytics-updated-at");
      if (node) {
        const generatedAt = new Date(snapshot.generated_at);
        node.textContent = Number.isNaN(generatedAt.getTime())
          ? "Updated now"
          : "Updated " + generatedAt.toLocaleTimeString();
      }
    }
  }

  let loading = false;

  function setRefreshButtonState(isLoading) {
    if (!refreshButton) {
      return;
    }
    refreshButton.disabled = isLoading;
    refreshButton.textContent = isLoading ? "Refreshing..." : "Refresh";
  }

  async function refresh(options) {
    const manual = Boolean(options && options.manual);
    if (loading) {
      return;
    }
    loading = true;
    if (manual) {
      setRefreshButtonState(true);
    }
    try {
      const response = await fetch(endpoint, {
        method: "GET",
        headers: {
          Accept: "application/json",
        },
        cache: "no-store",
      });
      if (!response.ok) {
        throw new Error("analytics request failed");
      }
      const payload = await response.json();
      render(payload || {});
    } catch (_err) {
      const updated = document.getElementById("analytics-updated-at");
      if (updated) {
        updated.textContent = "Update failed";
      }
    } finally {
      loading = false;
      if (manual) {
        setRefreshButtonState(false);
      }
    }
  }

  if (refreshButton) {
    refreshButton.addEventListener("click", function () {
      refresh({ manual: true });
    });
  }

  refresh();
  setInterval(function () {
    refresh({ manual: false });
  }, pollIntervalMs);
})();
