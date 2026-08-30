// The contact page chat, a scripted branching conversation with typing delays.
// The tree stays in JS rather than in Go with the rest of the copy, since none
// of it is ever rendered server-side.

const CHAT_TREE = {
  start: {
    messages: ["Hey there!", "What brings you here?"],
    options: [
      { label: "I want to work together", next: "collab" },
      { label: "Just checking out your work", next: "browsing" },
      { label: "Job opportunity", next: "job" },
    ],
  },
  collab: {
    messages: [
      "Nice! I'm always open to side projects and open source collabs.",
      "Best way to reach me is email. Drop me a line and tell me what you're thinking.",
    ],
    options: [
      { label: "What's your email?", next: "email" },
      { label: "What kind of projects?", next: "projects" },
    ],
  },
  browsing: {
    messages: [
      "Welcome! Feel free to look around.",
      "If anything catches your eye or you want to chat, I'm easy to reach.",
    ],
    options: [
      { label: "How do I reach you?", next: "email" },
      { label: "What are you working on?", next: "projects" },
    ],
  },
  job: {
    messages: [
      "I appreciate the interest.",
      "I'm currently at Craftmaster Furniture as a Senior Solutions Architect and not actively looking.",
      "That said, feel free to reach out if it's compelling.",
    ],
    options: [
      { label: "What's the best way to reach you?", next: "email" },
      { label: "What do you do there?", next: "work" },
    ],
  },
  email: {
    messages: [
      "Email is best: isaac@bythewood.me",
      "You can also find me on GitHub as /overshard or on Discord as Overshard#4907.",
    ],
    options: [{ label: "Thanks!", next: "end" }],
  },
  projects: {
    messages: [
      "Lately I've been deep into AI agent workflows, automated testing infrastructure, and tooling for fast release cycles.",
      "On the side I build self-hosted tools and experiment with whatever is new. Check out the Code page for more.",
    ],
    options: [
      { label: "How do I reach you?", next: "email" },
      { label: "Cool, thanks!", next: "end" },
    ],
  },
  work: {
    messages: [
      "I focus on AI agent workflows, automated integration testing, and building systems for rapid releases without sacrificing stability or security.",
      "Two decades of experience across the full stack, from kernel modules to regulated healthcare environments.",
    ],
    options: [
      { label: "How do I reach you?", next: "email" },
      { label: "Interesting, thanks!", next: "end" },
    ],
  },
  end: {
    messages: ["Anytime! Take care."],
    options: [],
  },
};

export const initContact = () => {
  const chat = document.querySelector(".contact-chat");
  // Handed over by the server, so the image format lives only in images.json.
  const AVATAR = chat?.dataset.avatar || "";
  const scroller = document.querySelector(".contact-gridRight");
  if (!chat) return;

  const scrollToEnd = () => {
    if (scroller) scroller.scrollTop = scroller.scrollHeight;
  };

  const avatar = () => {
    const span = document.createElement("span");
    span.className = "contact-chatAvatar";
    const img = document.createElement("img");
    img.src = AVATAR;
    img.alt = "Isaac";
    img.width = 40;
    img.height = 40;
    span.appendChild(img);
    return span;
  };

  const addMessage = (text, from) => {
    const line = document.createElement("div");
    line.className = "contact-chatLine";
    if (from === "user") line.classList.add("contact-chatLineUser");
    else line.appendChild(avatar());

    const bubble = document.createElement("div");
    bubble.className = "contact-chatBubble";
    if (from === "user") bubble.classList.add("contact-chatBubbleUser");
    bubble.append(document.createTextNode(text));

    if (from !== "user") {
      const name = document.createElement("span");
      name.textContent = "Isaac";
      bubble.appendChild(name);
    }

    line.appendChild(bubble);
    chat.appendChild(line);
    scrollToEnd();
  };

  const typingIndicator = () => {
    const line = document.createElement("div");
    line.className = "contact-chatLine";
    line.dataset.typing = "true";
    line.appendChild(avatar());

    const bubble = document.createElement("div");
    bubble.className = "contact-chatBubble";
    const dots = document.createElement("span");
    dots.className = "contact-typingDots";
    dots.append(
      document.createElement("span"),
      document.createElement("span"),
      document.createElement("span")
    );
    bubble.appendChild(dots);
    line.appendChild(bubble);
    return line;
  };

  const clearOptions = () => {
    const existing = chat.querySelector(".contact-chatOptions");
    if (existing) existing.remove();
  };

  const showOptions = (options) => {
    clearOptions();
    if (!options.length) return;

    const wrapper = document.createElement("div");
    wrapper.className = "contact-chatOptions";
    options.forEach((option) => {
      const button = document.createElement("button");
      button.className = "contact-chatOption";
      button.type = "button";
      button.textContent = option.label;
      button.addEventListener("click", () => {
        clearOptions();
        addMessage(option.label, "user");
        if (option.next) window.setTimeout(() => play(option.next), 400);
      });
      wrapper.appendChild(button);
    });
    chat.appendChild(wrapper);
    scrollToEnd();
  };

  // The typing indicator's dwell scales with message length, so a long reply
  // visibly takes longer to write than a short one.
  const play = (key) => {
    const node = CHAT_TREE[key];
    if (!node) return;

    const queue = [...node.messages];

    const next = () => {
      if (!queue.length) {
        showOptions(node.options);
        return;
      }

      const text = queue.shift();
      const indicator = typingIndicator();
      chat.appendChild(indicator);
      scrollToEnd();

      window.setTimeout(() => {
        indicator.remove();
        addMessage(text, "isaac");
        next();
      }, 600 + text.length * 15);
    };

    next();
  };

  // Waits out the panel entrance and content stagger in contact.css, so the
  // first typing indicator does not land on top of the left column arriving.
  window.setTimeout(() => play("start"), 1150);
};
