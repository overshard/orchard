// The snippet embedded on every tracked page. It sends session_start,
// page_view, click, scroll and page_leave, plus anything pushed onto
// window.collectorQueue as an {event, data} pair.

(function() {
  function set_cookie(name, value, expires) {
    var d = new Date();
    d.setTime(d.getTime() + (expires * 24 * 60 * 60 * 1000));
    expires = "expires=" + d.toUTCString();
    document.cookie = name + "=" + value + "; " + expires + "; path=/";
  }

  function get_cookie(name) {
    name = name + "=";
    var ca = document.cookie.split(";");
    for (var i = 0; i < ca.length; i++) {
      var c = ca[i];
      while (c.charAt(0) == " ") {
        c = c.substring(1);
      }
      if (c.indexOf(name) == 0) {
        return c.substring(name.length, c.length);
      }
    }
    return "";
  }

  var collectorUserId = get_cookie("collectoruserid");
  if (collectorUserId === "") {
    collectorUserId = Math.floor(Math.random() * 1000000000);
    set_cookie("collectoruserid", collectorUserId, 365);
    window.collectorQueue.push({
      collectorId: window.collectorId,
      event: "session_start",
      data: {
        user_id: collectorUserId,
        url: window.location.pathname,
        title: document.title,
        referrer: document.referrer,
        screen_width: window.screen.width,
        screen_height: window.screen.height,
        user_agent: "userAgent" in navigator ? navigator.userAgent : "",
        platform: "userAgentData" in navigator ? navigator.userAgentData.platform : "",
        device: "userAgentData" in navigator ? navigator.userAgentData.mobile ? "Mobile" : "Desktop" : "",
        browser: "userAgentData" in navigator ? navigator.userAgentData.brands[navigator.userAgentData.brands.length - 1].brand : "",
      },
    });
  }

  window.collectorQueue = {
    data: window.collectorQueue || [],
    post: function() {
      for (var i = 0; i < this.data.length; i++) {
        var data = this.data[i];
        if (!data.collectorId) {
          data.collectorId = window.collectorId;
        }
        if (!data.user_id) {
          data.user_id = collectorUserId;
        }
        // keepalive lets the pagehide-triggered page_leave survive the page
        // being torn down; without it browsers drop the request mid-unload.
        fetch(window.collectorServer + "/collect/", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
          },
          body: JSON.stringify(data),
          keepalive: true,
        });
      }
      this.data = [];
    },
    push: function(data) {
      this.data.push(data);
      this.post();
    },
  };

  function parse_querystring(querystring) {
    var query = {};
    var pairs = querystring.split("&");
    for (var i = 0; i < pairs.length; i++) {
      var pair = pairs[i].split("=");
      query[decodeURIComponent(pair[0])] = decodeURIComponent(pair[1]);
    }
    return query;
  }

  const query = parse_querystring(window.location.search.substring(1));

  // send a page view event. Screen dimensions are included here too (not just
  // session_start) so the dashboard's screen-size breakdown populates for
  // returning visitors whose collectoruserid cookie suppresses session_start.
  window.collectorQueue.push({
    collectorId: window.collectorId,
    event: "page_view",
    data: {
      user_id: collectorUserId,
      url: window.location.pathname,
      title: document.title,
      referrer: document.referrer,
      utm_source: query.utm_source,
      utm_medium: query.utm_medium,
      utm_campaign: query.utm_campaign,
      screen_width: window.screen.width,
      screen_height: window.screen.height,
    },
  });

  document.addEventListener("click", function (event) {
    window.collectorQueue.push({
      collectorId: window.collectorId,
      event: "click",
      data: {
        user_id: collectorUserId,
        url: window.location.pathname,
        title: document.title,
        x: event.clientX,
        y: event.clientY,
        target: event.target.tagName,
        // A click on <body> would otherwise ship the whole page's text.
        text: (event.target.textContent || "").slice(0, 200),
      },
    });
  });

  var last_scroll_event = 0;
  window.addEventListener("scroll", function () {
    if (new Date().getTime() - last_scroll_event > 1000) {
      window.collectorQueue.push({
        collectorId: window.collectorId,
        event: "scroll",
        data: {
          user_id: collectorUserId,
          url: window.location.pathname,
          title: document.title,
        },
      });
      last_scroll_event = new Date().getTime();
    }
  });

  // Only visible time counts, so a backgrounded tab does not inflate it. Flushed
  // on pagehide, which is more reliable than beforeunload on Safari and mobile.
  var visible_since = document.visibilityState === "visible" ? new Date().getTime() : null;
  var visible_accumulated = 0;
  var page_leave_sent = false;

  document.addEventListener("visibilitychange", function () {
    var now = new Date().getTime();
    if (document.visibilityState === "hidden" && visible_since !== null) {
      visible_accumulated += now - visible_since;
      visible_since = null;
    } else if (document.visibilityState === "visible" && visible_since === null) {
      visible_since = now;
    }
  });

  function send_page_leave() {
    if (page_leave_sent) return;
    page_leave_sent = true;
    var now = new Date().getTime();
    var time_on_page = visible_accumulated + (visible_since !== null ? now - visible_since : 0);
    window.collectorQueue.push({
      collectorId: window.collectorId,
      event: "page_leave",
      data: {
        user_id: collectorUserId,
        url: window.location.pathname,
        title: document.title,
        time_on_page: time_on_page,
      },
    });
  }

  window.addEventListener("pagehide", send_page_leave);
})();
