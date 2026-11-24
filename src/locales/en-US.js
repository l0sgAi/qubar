export default {
  // 通用
  common: {
    appName: 'Qubar',
    search: 'Search',
    searchPlaceholder: 'Search circles, posts...',
    searchPosts: 'Search Posts',
    searchCircles: 'Search Circles',
    searchUsers: 'Search Users',
    loading: 'Loading...',
    loadMore: 'Load More',
    noMore: 'No more',
    noResults: 'No results',
    noData: 'No data',
    cancel: 'Cancel',
    confirm: 'Confirm',
    save: 'Save',
    delete: 'Delete',
    edit: 'Edit',
    share: 'Share',
    back: 'Back',
    submit: 'Submit',
    logout: 'Logout',
    logoutConfirm: 'Are you sure you want to logout?',
    logoutSuccess: 'Logged out successfully',
    operationSuccess: 'Operation successful',
    operationFailed: 'Operation failed',
    unknownError: 'Unknown error',
    unknown: 'Unknown',
    featureInDevelopment: 'Feature in development...',
    collapse: 'Collapse',
    showMore: 'Show more',
    yes: 'Yes',
    enabled: 'Enabled'
  },

  // Mobile web portal (≤ 768px, see docs/mobile-adaptation-plan.md)
  mobile: {
    tab: {
      home: 'Home',
      discover: 'Discover',
      notice: 'Inbox',
      me: 'Me',
      login: 'Log in'
    },
    segment: {
      discover: 'Discover',
      hot: 'Trending'
    },
    linkCopied: 'Link copied',
    leaveConfirm: 'Leave "{name}"?',
    copyFailed: 'Copy failed, please copy the link from the address bar',
    unsupported: {
      title: 'Not available on mobile web yet',
      desc: 'Posting, editing and management are available on desktop. A mobile app is on the way.',
      copyLink: 'Copy link to open on a computer',
      desktopHint: 'You can also choose "Request desktop site" in your browser menu to switch to the desktop layout.',
      downloadApp: 'Get the app',
      backHome: 'Back to home'
    },
    comment: {
      placeholder: 'Write a comment...',
      replyPlaceholder: 'Reply to {name}',
      // Bottom compose bar / composer drawer (see MobileComposeBar, MobileCommentComposer)
      barPlaceholder: 'Say something…',
      replyTitle: "Reply to {'@'}{name}",
      toComments: 'View comments',
      send: 'Send',
      richHint: "Images and {'@'} mentions are available on desktop"
    },
    me: {
      settings: 'Settings',
      language: 'Language',
      terms: 'Terms of Service',
      privacy: 'Privacy Policy'
    }
  },

  // Browser tab titles (document.title, see applyPageTitle in router/index.js)
  title: {
    brand: 'Qubar',
    slogan: 'Discover Your Passion',
    success: 'Login Success',
    home: 'Home',
    hot: 'Hot',
    discover: 'Discover',
    createPost: 'Create Post',
    profile: 'Profile',
    userDetail: 'User Profile',
    terms: 'Terms of Service',
    privacy: 'Privacy Policy',
    searchResults: 'Search Results',
    searchKeyword: 'Search "{keyword}"',
    circleDetail: 'Circle Detail',
    circleMembers: 'Member Management',
    circleEdit: 'Edit Circle',
    postDetail: 'Post Detail',
    notifications: 'Messages',
    adminAgents: 'Agent Management',
    circleAgents: 'Circle Agents',
    circleModeration: 'Moderation',
    managedCircles: 'Circles I Manage',
    circleInvites: 'Invite Members',
    invite: 'Circle Invitation',
    myInvites: 'Circle Invitations',
    following: 'Following',
    followers: 'Followers',
    mutual: 'Mutual Follows',
    messages: 'Messages',
    chat: 'Chat',
    // Dynamic title templates: show concrete content after data loads (see utils/pageTitle.js)
    postDetailName: '{name}',
    circleDetailName: '{name}',
    userDetailName: '{name}',
    circleMembersName: 'Member Management: {name}',
    circleEditName: 'Edit Circle: {name}',
    circleAgentsName: 'Agent Management: {name}',
    circleModerationName: 'Moderation: {name}',
    circleInvitesName: 'Invite Members: {name}',
    inviteName: 'Join {name}',
    profileName: 'Profile: {name}',
    notFound: 'Page Not Found'
  },

  // 404 page
  notFound: {
    title: 'Page Not Found',
    description: 'The page you are looking for does not exist or has been removed. Please check the address.',
    backHome: 'Back to Home'
  },

  // 时间相关
  time: {
    justNow: 'Just now',
    minutesAgo: '{minutes}m ago',
    hoursAgo: '{hours}h ago',
    daysAgo: '{days}d ago',
    monthsAgo: '{months}mo ago',
    yearsAgo: '{years}y ago',
    createdAt: 'Created',
    updatedAt: 'Updated'
  },

  // 帖子相关
  post: {
    post: 'Post',
    createPost: 'Create Post',
    createPostSuccess: 'Created successfully',
    createPostFailed: 'Creation failed',
    title: 'Title',
    titlePlaceholder: 'Enter post title',
    content: 'Content',
    contentPlaceholder: 'Enter post content (Markdown supported)',
    summary: 'Summary',
    summaryPlaceholder: 'Enter summary for search and keyword matching',
    belongToCircle: 'Circle',
    selectCircle: 'Search and select a circle',
    circleRules: 'Circle Rules',
    noCircleRules: 'No rules for this circle',
    selectCircleToViewRules: 'Select a circle to view rules',
    detail: 'Post Detail',
    status: {
      normal: 'Normal',
      draft: 'Draft',
      reviewing: 'Reviewing',
      rejected: 'Not approved',
      blocked: 'Hidden, pending review',
      // Status tag explanations (hover / tap); rejected / blocked are also the author's banner on post detail
      hint: {
        rejected: "Only you can see this post. It didn't pass AI review or was rejected / removed by a circle admin. Check Notifications for details.",
        blocked: 'This post was reported several times and is temporarily hidden while a circle admin reviews it.'
      }
    },
    badges: {
      pinned: 'Pinned',
      essence: 'Essence',
      locked: 'Locked'
    },
    stats: {
      views: 'Views',
      comments: 'Comments',
      likes: 'Likes',
      favorites: 'Favorites'
    },
    actions: {
      like: 'Like',
      liked: 'Liked',
      favorite: 'Favorite',
      favorited: 'Favorited',
      share: 'Share',
      comment: 'Comment'
    },
    recent: {
      title: 'Recent Posts',
      subtitle: 'Latest published posts',
      loadMore: 'Load More',
      empty: 'No recent posts yet'
    },
    noPosts: 'No posts yet',
    noPostResults: 'No post results',
    noFavorites: 'No favorites yet',
    searching: 'Searching...',
    hottest:'Trending',
    latest:'Latest',
    highlights:'Highlights',
    postNotFound: 'Post not found',
    viewDetail: 'View details',
    loadCirclesFailed: 'Failed to load circles',
    publishSuccess: 'Published successfully',
    publishNoId: 'Post ID not received, returning to home',
    publishFailed: 'Publish failed, please try again',
    submittedForReview: "Submitted. It's being reviewed and will be visible to others once approved.",
    // Post doesn't exist, or a non-author opened an unpublished post (must not reveal review state)
    unavailable: "This post doesn't exist or isn't available right now.",
    // Under-review banner shown to the author
    reviewing: {
      banner: 'This post is under review and only visible to you.',
      refresh: 'Refresh',
      published: 'Approved. Your post is now public.',
      slow: "Review is taking longer than usual. We'll notify you in Notifications when it's done."
    },
    // Report a post (POST /post/report); reason labels live in top-level report.reasons
    report: {
      action: 'Report',
      reported: 'Reported',
      more: 'More actions',
      title: 'Report post',
      reasonLabel: 'Reason',
      detailPlaceholder: 'Add details (optional)',
      anonymousNote: "The author won't see who reported this post.",
      // Care hint shown when "Self-harm" is picked (copy pending product review)
      selfHarmHint: "If you or someone you know is going through a hard time, please reach out to someone you trust or a local crisis line. You're not alone.",
      submit: 'Submit report',
      success: 'Report submitted. Thanks for letting us know.',
      already: "You've already reported this post.",
      tooOften: "You're reporting too often. Please try again later.",
      unavailable: "This post doesn't exist or can't be reported.",
      invalid: "You can't report your own post, or the request is invalid.",
      failed: "Couldn't submit the report. Please try again later."
    },
    validation: {
      circleRequired: 'Please select a circle',
      titleRequired: 'Please enter a title',
      contentRequired: 'Please enter content'
    }
  },

  // 圈子相关
  circle: {
    circle: 'Circle',
    interestCircle: 'Interest Circle',
    circles: 'Circles',
    myCircles: 'My Circles',
    joinCircle: 'Join Circle',
    leaveCircle: 'Leave Circle',
    joined: 'Joined',
    active: 'Active',
    random: 'Random',
    viewAll: 'View All',
    myCircles: 'My Circles',
    createCircle: 'Create Circle',
    createCircleSuccess: 'Created successfully',
    joinSuccess: 'Joined successfully',
    // POST /circle/join failures (mapped by code, backend message not shown)
    joinError: {
      alreadyMember: 'You are already a member of this circle',
      inviteOnly: 'This private circle is invite only',
      banned: 'You have been banned from this circle',
      notFound: 'This circle does not exist or has been deleted',
      generic: 'Failed to join. Please try again later.'
    },
    // Private circle, viewer is not a member (backend #170)
    private: {
      inviteOnly: 'Private circle, invite only',
      membersOnly: 'Private circle, members only',
      membersOnlyDesc: 'Posts and details are only visible to members. Join by invitation to see them.',
      restrictedNote: 'Description, rules and other details are visible to members only',
      inviteHint: 'Invite only. Ask the owner or an admin for an invite link.'
    },
    // Private circle invitations (backend v0.7.0, #169)
    invite: {
      title: 'Invite Members',
      entry: 'Invite',
      publicCircle: 'Public circles need no invitation. Anyone can join directly.',
      link: {
        title: 'Invite Links',
        desc: 'Share a link in any chat app. People who open it can join this circle after signing in and confirming.',
        create: 'Create Link',
        createTitle: 'Create Invite Link',
        expire: 'Expires in',
        expireDays: '{n} days',
        hoursUnit: 'hours',
        expireHint: '1 hour to 30 days, 7 days by default',
        maxUses: 'Max uses',
        maxUsesPlaceholder: 'Unlimited',
        maxUsesHint: 'Leave empty for unlimited, 1 to 10000',
        createdTitle: 'Invite link created',
        onlyOnce: 'This link is shown only once. Copy it now, it cannot be viewed again after closing.',
        copy: 'Copy Link',
        copied: 'Invite link copied',
        copyFailed: 'Copy failed. Please select the link and copy it manually.',
        done: 'I have saved it',
        usesLimit: 'Up to {n} uses',
        usesUnlimited: 'Unlimited uses',
        empty: 'No invite links yet',
        expireAt: 'Expires {time}',
        used: 'Used {used} / {max}',
        usedUnlimited: 'Used {used} · unlimited',
        createdAt: 'Created {time}',
        unknownCreator: 'Unknown user',
        status: {
          active: 'Active',
          expired: 'Expired',
          revoked: 'Revoked',
          usedUp: 'Used up'
        },
        revoke: 'Revoke',
        revokeTitle: 'Revoke Invite Link',
        revokeContent: 'The link stops working immediately. Members who already joined with it are not affected. Revoke it?',
        revoked: 'Invite link revoked',
        loadFailed: 'Failed to load invite links',
        error: {
          notPrivate: 'Only private circles can create invite links',
          badRequest: 'Expiry or max uses is out of range',
          notFound: 'This invite link no longer exists. The list has been refreshed.',
          generic: 'Operation failed, please try again later'
        }
      },
      landing: {
        title: 'You are invited to a circle',
        memberCount: '{n} members',
        privateTag: 'Private',
        desc: 'Join to see posts in this circle and take part in discussions',
        join: 'Join Circle',
        loginToJoin: 'Sign in to Join',
        loginHint: 'You will come back to this page after signing in',
        joined: 'Joined {name}',
        alreadyMember: 'You are already a member of this circle',
        invalidTitle: 'Invitation no longer valid',
        invalidDesc: 'Ask the person who invited you for a new invite link',
        loadFailed: 'Failed to load the invitation. Check your network and try again.',
        retry: 'Retry',
        goDiscover: 'Discover more circles',
        error: {
          banned: 'You have been banned from this circle and cannot join',
          invalid: 'Invitation no longer valid',
          generic: 'Failed to join, please try again later'
        }
      },
      tabs: {
        link: 'Invite Links',
        email: 'Email Invites'
      },
      email: {
        title: 'Email Invites',
        desc: 'Invite specific people by their account email. No email is sent. They confirm in "Circle Invitations" after signing in, and emails not yet registered see it after signing up.',
        placeholder: 'One email per line, or separated by commas. Up to 50 at a time.',
        count: '{n} / {max}',
        add: 'Add Invites',
        emptyInput: 'Enter the emails to invite',
        tooMany: 'Up to {max} emails at a time, you entered {n}',
        addedCount: '{n} invites added',
        noneAdded: 'No new invites added',
        ignoredTitle: 'These emails were not added',
        ignored: {
          invalid: 'Invalid format',
          duplicate: 'Repeated in this batch',
          already_invited: 'Already has a pending or accepted invite'
        },
        filter: {
          all: 'All',
          pending: 'Pending',
          accepted: 'Accepted',
          rejected: 'Declined',
          revoked: 'Revoked'
        },
        invitedBy: 'Invited by {name}',
        createdAt: 'Invited {time}',
        updatedAt: 'Updated {time}',
        unknownInviter: 'Unknown user',
        empty: 'No email invites yet',
        emptyFiltered: 'No invites with this status',
        loadFailed: 'Failed to load email invites',
        revoke: 'Revoke',
        revokeTitle: 'Revoke Email Invite',
        revokeContent: '{email} will no longer see this invitation. Revoke it?',
        revoked: 'Invite revoked',
        error: {
          handled: 'This invite has already been handled',
          notPrivate: 'Only private circles can add email invites',
          badRequest: 'Enter between 1 and 50 emails',
          notFound: 'This invite no longer exists. The list has been refreshed.',
          generic: 'Operation failed, please try again later'
        }
      },
      // My invitations: the backend sends no email or notification, so this is the only entry point
      mine: {
        title: 'Circle Invitations',
        navEntry: 'Invitations',
        desc: 'Invitations sent to your account email by circle owners or admins appear here',
        entryPending: 'You have {n} pending circle invitations',
        entryNone: 'No pending circle invitations',
        entryAction: 'View',
        invitedBy: '{name} invited you to join',
        unknownInviter: 'A circle admin',
        accept: 'Accept',
        reject: 'Decline',
        rejectTitle: 'Decline Invitation',
        rejectContent: 'This invitation will no longer be shown. Decline joining {name}?',
        accepted: 'Joined {name}',
        alreadyMember: 'You are already a member of this circle',
        rejected: 'Invitation declined',
        empty: 'No pending circle invitations',
        loadFailed: 'Failed to load invitations, please try again later',
        retry: 'Retry',
        error: {
          invalid: 'Invitation no longer valid',
          banned: 'You have been banned from this circle and cannot join',
          generic: 'Operation failed, please try again later'
        }
      }
    },
    leaveSuccess: 'Left circle',
    leaveFailed: 'Failed to leave',
    disturb: 'Mute',
    disturbOn: 'Mute On',
    disturbOff: 'Mute',
    disturbEnabled: 'Mute enabled',
    disturbDisabled: 'Mute disabled',
    createPost: 'Post',
    members: 'Members',
    posts: 'Posts',
    membersShort: 'Members',
    postsShort: 'Posts',
    description: 'Description',
    noCircles: 'No circles yet',
    noCircleResults: 'No circle results',
    resultsMayBeIncomplete: 'Results may be incomplete, refine your keyword',
    moreOptions: 'More',
    report: 'Report',
    joinType: 'Join Type',
    joinTypeDirect: 'Direct Join',
    joinTypePrivate: 'Private Circle',
    memberInfo: 'Member Info',
    roleMember: 'Member',
    roleAdmin: 'Admin',
    roleOwner: 'Owner',
    statusMuted: 'Muted',
    rules: 'Circle Rules',
    aboutCircle: 'About Circle',
    hotness: 'Hotness',
    noDescription: 'No description',
    yourMemberInfo: 'Your Member Info',
    pinnedDisplay: 'Pinned',
    messageDisturb: 'Notifications Muted',
    shareCircle: 'Share Circle',
    circleIdNotFound: 'Circle ID not found',
    statusNormal: 'Normal',
    statusBanned: 'Banned',
    statusExited: 'Exited',
    // Joined circles list (my profile / user profile)
    joinedList: {
      roleAll: 'All',
      roleOwner: 'Owner',
      roleAdmin: 'Admin',
      roleMember: 'Member',
      muted: 'Muted',
      mutedUntil: 'Muted · until {time}',
      loadFailed: 'Failed to load circles',
      retry: 'Retry',
      empty: {
        mine: {
          all: "You haven't joined any circles yet",
          owner: "You don't own any circles yet",
          admin: "You aren't an admin of any circle yet",
          member: "You haven't joined any circles as a regular member yet"
        },
        others: {
          all: "They haven't joined any circles yet",
          owner: "They don't own any circles yet",
          admin: "They aren't an admin of any circle yet",
          member: "They haven't joined any circles as a regular member yet"
        }
      }
    },
    manage: {
      title: 'Member Management',
      entry: 'Manage',
      backToCircle: 'Back to Circle',
      viewCircle: 'View Circle',
      tabs: {
        normal: 'Members',
        muted: 'Muted',
        banned: 'Banned'
      },
      badgeFull: '20+',
      empty: 'No members',
      loadFailed: 'Failed to load members',
      searchPlaceholder: 'Search username / email',
      noSearchResult: 'No matching members',
      searchUnavailable: 'Search is temporarily unavailable, please try again later',
      joinAt: 'Joined {time}',
      muteRemaining: 'Muted for {time}',
      remainingMinutes: '{n} min',
      remainingHours: '{n} h',
      remainingDays: '{n} d',
      unknownUser: 'Unknown User',
      noPermissionVisit: 'No permission to access member management',
      actions: {
        mute: 'Mute',
        unmute: 'Unmute',
        remove: 'Remove',
        ban: 'Ban',
        unban: 'Unban',
        setAdmin: 'Make Admin',
        removeAdmin: 'Remove Admin',
        transfer: 'Transfer Ownership'
      },
      muteTitle: 'Mute Member',
      muteTarget: 'Mute {name} from posting',
      muteDuration: 'Mute Duration',
      hoursUnit: 'hours',
      mutePreset1h: '1 hour',
      mutePreset24h: '24 hours',
      mutePreset7d: '7 days',
      mutePreset30d: '30 days',
      muteRangeError: 'Mute duration must be between 1 and 720 hours',
      confirm: {
        removeTitle: 'Remove Member',
        removeContent: 'Remove {name} from this circle? They will not be banned: they can rejoin a public circle, but a private circle needs a new invitation.',
        banTitle: 'Ban Member',
        banContent: 'Remove and ban {name}? They cannot join this circle again until unbanned.',
        unbanTitle: 'Unban Member',
        unbanContent: 'Unban {name}? They will not rejoin automatically: they can rejoin a public circle, but a private circle needs a new invitation.',
        removeAdminTitle: 'Remove Admin',
        removeAdminContent: 'Remove {name} from the admin role?',
        transferTitle: 'Transfer Ownership',
        transferContent: 'Transfer circle ownership to {name}? This is irreversible and you will become a regular member immediately.'
      },
      success: {
        muted: 'Member muted',
        unmuted: 'Mute removed',
        removed: 'Member removed',
        banned: 'Member banned',
        unbanned: 'Ban removed',
        adminSet: 'Admin role granted',
        adminRemoved: 'Admin role removed',
        transferred: 'Ownership transferred'
      },
      error: {
        conflict: 'Member status has changed. List refreshed, please retry',
        noPermission: 'No permission for this action',
        roleDemoted: 'Your management role has changed',
        memberNotFound: 'Member not found or no longer in this circle',
        badRequest: 'Invalid request parameters',
        generic: 'Operation failed, please try again later'
      }
    },
    // Circles I manage (fallback for report notices; pick a circle to moderate)
    managed: {
      title: 'Circles I Manage',
      subtitle: 'Pick a circle to review its reported and pending posts',
      statusPending: 'Circle under review; not manageable yet',
      statusBanned: 'Circle banned; not manageable'
    },
    moderation: {
      title: 'Moderation',
      noPermissionVisit: "You don't have permission to access moderation.",
      tabs: {
        pending: 'Pending review',
        reported: 'Reported'
      },
      empty: 'No posts waiting for review.',
      loadFailed: 'Failed to load posts pending review',
      unknownUser: 'Unknown user',
      untitled: '(Untitled)',
      postedAt: 'Posted {time}',
      showContent: 'Show full post',
      hideContent: 'Hide full post',
      ai: {
        title: 'AI verdict',
        pending: 'No AI verdict yet. You can review it manually.',
        failed: "AI couldn't reach a verdict.",
        noHit: 'No risks flagged',
        hazardTag: '{label} {percent}%',
        severity: 'Severity: {level} ({score})',
        confidence: '{percent}% confidence',
        model: 'Model: {model}',
        showHazards: 'Show all risk scores',
        hideHazards: 'Hide risk scores',
        care: 'This author may need support.'
      },
      hazards: {
        sexual: 'Sexual content',
        violence_terror: 'Violence / terrorism',
        harassment: 'Harassment',
        illegal: 'Illegal activity',
        spam_ad: 'Spam / ads',
        privacy: 'Privacy leak',
        self_harm: 'Self-harm',
        manipulation: 'Manipulation'
      },
      severity: {
        none: 'None',
        minor: 'Minor',
        severe: 'Severe',
        extreme: 'Extreme'
      },
      decision: {
        publish: 'Passed by AI',
        review_publish: 'AI was unsure; auto-published',
        fail_open: 'AI unavailable, published'
      },
      actions: {
        approve: 'Approve',
        reject: 'Reject'
      },
      confirm: {
        approveTitle: 'Approve post',
        approveContent: 'Approve "{title}"? It will become public in the circle, and the author will be notified.',
        rejectTitle: 'Reject post',
        rejectContent: 'Reject "{title}"? Only the author will be able to see it, and they will be notified.',
        reasonLabel: 'Note (optional)',
        reasonPlaceholder: 'Add a reason',
        reasonHint: "Notes are only kept in the moderation log and aren't shown to the author."
      },
      success: {
        approved: 'Approved. The post will appear in the circle in a few seconds.',
        rejected: 'Rejected.'
      },
      error: {
        handled: 'This post has already been handled.',
        noPermission: "You don't have permission to do this.",
        badRequest: 'Invalid request',
        generic: 'Something went wrong. Please try again later.'
      },
      reported: {
        empty: 'No reported posts.',
        loadFailed: 'Failed to load reported posts',
        reportCount: 'Reported {n} times',
        reportCountOne: 'Reported once',
        firstReportAt: 'First reported {time}',
        autoHidden: 'Auto-hidden',
        autoHiddenHint: 'Only visible to the author. Keeping it will make it public again.',
        reasonsTitle: 'Report reasons',
        onlyRecent: 'Showing the {n} most recent reports',
        reasonCount: '{label} ×{n}',
        showReports: 'Show report details',
        hideReports: 'Hide report details',
        focusHandled: 'This report has already been handled',
        handled: 'Another moderator has already handled this post.',
        actions: {
          keep: 'Keep',
          remove: 'Remove'
        },
        confirm: {
          keepTitle: 'Keep post',
          keepContent: 'Keep "{title}"? All open reports on it will be closed, and it stays public.',
          removeTitle: 'Remove post',
          removeContent: 'Remove "{title}"? Removed posts are only visible to the author, who will be notified.'
        },
        success: {
          kept: 'Kept. Closed {n} reports.',
          keptOne: 'Kept. Closed 1 report.',
          removed: 'Removed.'
        }
      }
    },
    edit: {
      title: 'Edit Circle',
      ownerSection: 'Owner Only',
      ownerSectionDesc: 'Only the circle owner can modify these fields',
      adminSection: 'Basic Info',
      adminSectionDesc: 'Both owner and admins can modify',
      changedCount: '{n} field(s) changed, only changes will be submitted',
      noChanges: 'No changes yet',
      removeImage: 'Remove',
      slugClearHint: 'Leave empty and save to clear the identifier',
      ruleClearHint: 'Leave empty and save to clear the circle rules',
      saveSuccess: 'Circle profile saved',
      cacheHint: 'Note: circle detail page and search results may take a few seconds to update',
      saveFailed: 'Save failed, please try again later',
      nameConflict: 'Name or identifier conflicts with another circle',
      noPermission: 'No permission for this action, your role may have changed',
      validationFailed: 'Validation failed, please check your changes',
      validation: {
        nameLength: 'Name must be 1-50 characters',
        descriptionMax: 'Description cannot exceed 2000 characters',
        ruleMax: 'Rules cannot exceed 2000 characters'
      }
    },
    form: {
      name: 'Circle Name',
      namePlaceholder: 'Enter circle name',
      slug: 'Unique Identifier',
      slugPlaceholder: 'Auto-generate or enter manually (lowercase letters, numbers, hyphens only)',
      description: 'Description',
      descriptionPlaceholder: 'Briefly describe this interest circle...',
      avatar: 'Avatar',
      avatarUpload: 'Click to Upload',
      avatarTip: 'Recommended size 200x200px',
      cover: 'Cover Image',
      coverUpload: 'Click to Upload',
      coverTip: 'Recommended size 1200x300px',
      rules: 'Circle Rules',
      rulesPlaceholder: 'Enter behavioral guidelines for the circle...',
      category: 'Category',
      categoryPlaceholder: 'Select a category',
      joinType: 'Join Type',
      joinTypeDirect: 'Direct Join',
      joinTypeDirectDesc: 'Anyone can join directly',
      joinTypePrivate: 'Private Circle',
      joinTypePrivateDesc: 'Invite only',
      createButton: 'Create Circle',
      step1Title: 'Basic Info',
      step2Title: 'Images & Rules',
      step3Title: 'Category & Access',
      step4Title: 'Review',
      nextStep: 'Next',
      prevStep: 'Previous',
      cropAvatarTitle: 'Crop Avatar',
      cropCoverTitle: 'Crop Cover',
      cropConfirm: 'Confirm Crop',
      cropReset: 'Reset',
      previewButton: 'Preview Effect',
      previewTitle: 'Effect Preview',
      previewHint: 'Drag the crop box to preview the header live',
      previewNamePlaceholder: 'Circle name (sample)',
      validation: {
        nameRequired: 'Please enter circle name',
        nameLength: 'Name length should be between 2-50 characters',
        slugPattern: 'Can only contain lowercase letters, numbers and hyphens',
        slugMaxLength: 'Maximum 60 characters',
        descriptionRequired: 'Please enter description',
        descriptionLength: 'Description length should be between 10-2000 characters',
        rulesRequired: 'Please enter circle rules',
        rulesLength: 'Rules length should be between 10-2000 characters',
        categoryRequired: 'Please select a category',
        pleaseComplete: 'Please complete the required fields before submitting'
      },
      messages: {
        avatarUploadSuccess: 'Avatar uploaded successfully',
        avatarUploadFailed: 'Avatar upload failed',
        coverUploadSuccess: 'Cover uploaded successfully',
        coverUploadFailed: 'Cover upload failed',
        uploadFailedRetry: 'Upload failed, please try again',
        createSuccess: 'Circle created successfully!',
        createFailed: 'Creation failed, please try again',
        loadCategoriesFailed: 'Failed to load categories',
        createNetworkError: 'Creation failed, please check network connection',
        cropFailed: 'Crop failed, please try again'
      }
    }
  },

  // 用户相关
  user: {
    user: 'User',
    username: 'Username',
    avatar: 'Avatar',
    profile: 'Profile',
    editProfile: 'Edit Profile',
    myPosts: 'My Posts',
    posts: 'Posts',
    myFavorites: 'My Favorites',
    browseHistory: 'History',
    myGroups: 'Circles',
    userInfo: 'User Info',
    basicInfo: 'Basic Info',
    email: 'Email',
    phone: 'Phone',
    gender: 'Gender',
    birthday: 'Birthday',
    role: 'Role',
    status: 'Status',
    notSet: 'Not set',
    notBound: 'Not bound',
    normal: 'Normal',
    disabled: 'Disabled',
    userSearchInDevelopment: 'User search in development...',
    anonymous: 'Anonymous',
    unknownUser: 'Unknown User',
    idNotFound: 'User ID not found',
    fetchFailed: 'Failed to fetch user info',
    admin: 'Admin',
    noUserResults: 'No user results',
    today: 'Today',
    yesterday: 'Yesterday',
    daysAgo: '{days}d ago',
    monthsAgo: '{months}mo ago',
    yearsAgo: '{years}y ago',
    genders: {
      notSet: 'Not set',
      male: 'Male',
      female: 'Female',
      other: 'Other'
    },
    roles: {
      user: 'User',
      admin: 'Admin',
      superAdmin: 'Super Admin',
      agentBot: 'Agent Bot'
    },
    editModal: {
      title: 'Edit Profile',
      usernamePlaceholder: 'Enter username (1-50 characters)',
      phonePlaceholder: 'Enter phone number',
      genderPlaceholder: 'Select gender',
      birthdayPlaceholder: 'Select birthday',
      clickUploadAvatar: 'Click to upload avatar',
      cropAvatarTitle: 'Crop Avatar',
      previewTitle: 'Avatar Preview',
      previewHint: 'Drag the crop box to preview the avatar live',
      avatarTip: 'Avatar supports JPG, JPEG, PNG, GIF, WebP, SVG formats, max 10MB',
      saveChanges: 'Save Changes',
      usernameLengthError: 'Username must be 1-50 characters',
      phoneFormatError: 'Please enter a valid phone number',
      avatarTypeError: 'Only JPG, JPEG, PNG, GIF, WebP, SVG formats are supported',
      avatarSizeError: 'Image size cannot exceed 10MB',
      avatarUploadSuccess: 'Avatar uploaded successfully',
      avatarUploadFailed: 'Avatar upload failed',
      noChangesWarning: 'Please modify at least one field',
      updateSuccess: 'Profile updated successfully',
      updateFailed: 'Failed to update profile',
      fetchFailed: 'Failed to fetch user info',
      clearHistory: 'Clear browsing history'
    },
    passwordModal: {
      button: 'Change Password',
      title: 'Change Password',
      newPassword: 'New Password',
      confirmPassword: 'Confirm Password',
      newPasswordPlaceholder: 'Enter new password (at least 6 characters)',
      confirmPasswordPlaceholder: 'Enter the new password again',
      lengthError: 'Password must be at least 6 characters',
      mismatchError: 'The two passwords do not match',
      updateSuccess: 'Password changed successfully',
      updateFailed: 'Failed to change password',
      sessionTip: 'Your current session stays valid after changing the password. No need to log in again.'
    }
  },

  // 导航和菜单
  nav: {
    home: 'Home',
    recommend: 'Recommend',
    following: 'Following',
    hot: 'Hot',
    latest: 'Latest',
    discover: 'Discover',
    create: 'Create',
    messages: 'Messages',
    notifications: 'Notifications',
    settings: 'Settings',
    profile: 'Profile',
    searchResults: 'Search Results',
    keyword: 'Keyword'
  },

  // AI agent management (/admin/agents; admins see global+circles tabs, regular users see managed circles only)
  agent: {
    title: 'Agent Management',
    navEntry: 'Agents',
    noPermission: 'Access denied: admins only',
    tabs: {
      global: 'Global Agents',
      circles: 'Managed Circles'
    },
    circleList: {
      searchPlaceholder: 'Search circle name or description',
      roleOwner: 'Owner',
      roleAdmin: 'Admin',
      statusPending: 'Circle under review; agent management unavailable',
      statusBanned: 'Circle banned; agent management unavailable',
      agentsBound: '{count}/5 bound',
      members: '{count} members',
      posts: '{count} posts',
      noDescription: 'No description yet',
      empty: 'You have no manageable circles',
      emptySearch: 'No matching circles'
    },
    circlePage: {
      titleWithName: '{name} · Agents',
      backToList: 'Back to list',
      create: 'New Agent',
      edit: 'Edit Agent',
      quota: '{count}/5 bound',
      quotaFull: 'Up to 5 agents per circle; delete one to free a slot',
      noPermission: 'Access denied: only the circle owner/admins can manage circle agents',
      empty: 'No agents in this circle yet — create the first one',
      emptySearch: 'No matching agents',
      loadFailed: 'Failed to load circle agents',
      replyNotLive: 'Circle agents do not reply to posts yet (keyword/manual triggers are inactive); they are saved as configuration only. Replies will arrive in a later release.',
      credentialsOwnerOnly: 'Protocol, base URL and API key can only be changed by the circle owner.'
    },
    searchPlaceholder: 'Search agents by name',
    create: 'New Agent',
    edit: 'Edit Agent',
    unlimited: '∞',
    deleteSuccess: 'Deleted',
    deleteConfirm: 'Delete "{name}"? This cannot be undone (soft delete, no restore).',
    statusUpdateFailed: 'Failed to update status',
    triggerModes: {
      all: 'All new posts',
      keyword: 'Keywords',
      manual: 'Manual'
    },
    table: {
      name: 'Agent',
      protocol: 'Protocol',
      trigger: 'Trigger',
      rateLimit: 'Rate Limit',
      status: 'Status',
      createTime: 'Created At',
      actions: 'Actions'
    },
    form: {
      sectionBasic: 'Basic Info',
      sectionApi: 'Model Access',
      sectionTrigger: 'Trigger & Rate Limit',
      name: 'Name',
      namePlaceholder: '1-50 chars, globally unique',
      namePlaceholderCircle: '1-50 chars, unique in this circle',
      avatarUrl: 'Avatar',
      avatarUpload: 'Upload Avatar',
      avatarRemove: 'Remove',
      cropAvatarTitle: 'Crop Bot Avatar',
      avatarTypeError: 'Only JPEG / PNG / GIF / WebP / SVG are supported',
      avatarSizeError: 'Image must be under 10MB',
      avatarUploadFailed: 'Avatar upload failed, please retry',
      protocol: 'API Protocol',
      baseUrl: 'Custom API Base URL',
      baseUrlPlaceholder: 'Leave empty to use the official endpoint',
      apiKey: 'API Key',
      apiKeyPlaceholder: 'sk-...',
      apiKeyCurrent: 'Configured: {mask}',
      apiKeyNone: 'No key configured',
      apiKeyNewPlaceholder: 'Enter a new key to replace; leave empty to keep',
      apiKeyClear: 'Clear the saved key',
      model: 'Model',
      modelPlaceholder: 'e.g. gpt-4o-mini / claude-sonnet-5',
      llmParams: 'LLM Params (replaced as a whole)',
      systemPrompt: 'System Prompt',
      systemPromptPlaceholder: 'Persona and reply requirements (optional)',
      keywords: 'Trigger Keywords',
      keywordsPlaceholder: 'Type a keyword and press Enter',
      keywordsRequired: 'At least one trigger keyword is required',
      filterPrompt: 'Reply Filter (optional)',
      filterPromptPlaceholder: 'e.g. Only reply to programming questions. Leave empty to reply on any keyword hit',
      maxReplies: 'Max replies / hour',
      minInterval: 'Min interval',
      perHour: '/h',
      seconds: 's',
      noteTitle: 'Note',
      note: 'A comment containing any keyword triggers a reply. With a reply filter set, the LLM first decides whether to reply (rejected judgments don\'t consume rate-limit quota).',
      atLeastOne: 'Modify at least one field',
      createSuccess: 'Created',
      updateSuccess: 'Saved'
    },
    // Auto-reply to trending posts / comments (auto_config.auto_reply)
    autoReply: {
      section: 'Auto Reply',
      enabled: 'Auto-reply to trending content',
      enabledHint: 'When on, a background scan replies to posts / comments that reach the thresholds',
      targets: 'Reply to',
      targetPost: 'Posts (top-level comment)',
      targetComment: 'Comments (reply in the thread)',
      postHotMin: 'Post hotness threshold',
      postHotMinPlaceholder: 'Reply when hotness ≥ this',
      commentThreshold: 'Comment thresholds (either one is enough, 0 = ignored)',
      commentLikeMin: 'Likes ≥',
      commentReplyMin: 'Replies ≥',
      windowHours: 'Time window',
      windowHoursPlaceholder: 'Only content from the last N hours',
      hours: 'h',
      maxPerHour: 'Max auto replies / hour',
      maxPerHourPlaceholder: '0 = no separate limit',
      note: 'Each bot auto-replies to the same target at most once (failures and filter rejections count too; no retry). Auto replies still obey "Max replies / hour" and "Min interval", and go through the reply filter if one is set. Nothing happens while the platform-wide switch is off.',
      scopeGlobal: 'Global bots scan the whole site.',
      scopeCircle: 'Circle bots only reply to content in this circle.',
      errTargetsInvalid: 'Targets must be posts / comments without duplicates',
      errTargetsRequired: 'Pick at least one target when enabled',
      errThresholdRange: 'Thresholds must be non-negative integers',
      errWindowRange: 'Time window must be 1-168 hours',
      errMaxPerHourRange: 'Max per hour must be 0-1000',
      errPostHotRequired: 'Post hotness threshold must be ≥ 1 when replying to posts',
      errCommentThresholdRequired: 'Set likes or replies threshold ≥ 1 when replying to comments'
    }
  },

  // Trending page (/hot)
  trending: {
    title: 'Trending',
    window24h: '24h',
    window7d: '7d',
    posts: 'Trending Posts',
    circles: 'Trending Circles',
    users: 'Trending Users',
    refreshedJustNow: 'Updated just now',
    refreshedMinutesAgo: 'Updated {n} min ago',
    refreshedHoursAgo: 'Updated {n} h ago',
    refreshedNever: 'No data yet',
    empty: 'No trending data',
    loadFailed: 'Failed to load trending'
  },

  // Discover page (/discover): random circles + posts to expand your interests
  discover: {
    title: 'Discover',
    subtitle: 'Step out of your bubble and explore content you haven\'t seen',
    modeStream: 'Stream',
    modeSectioned: 'Lists',
    modeWall: 'Grid',
    circlesSection: 'Discover Circles',
    postsSection: 'Discover Posts',
    exploreHint: 'Explore',
    joinDirect: 'Open',
    joinPrivate: 'Private',
    refreshed: 'Fresh content just dropped in',
    refresh: 'Refresh',
    refreshing: 'Refreshing…',
    empty: 'Nothing to discover yet. Try Refresh.',
    loadFailed: 'Failed to load discover content',
    members: 'members',
    posts: 'posts'
  },

  // 登录页
  login: {
    tagline: 'Connect interests, discover possibilities',
    googleLogin: 'Continue with Google',
    githubLogin: 'Continue with GitHub',
    microsoftLogin: 'Continue with Microsoft',
    loginAgreement: 'By logging in, you agree to our',
    userAgreement: 'User Agreement',
    privacyPolicy: 'Privacy Policy',
    and: 'and',

    tabs: {
      login: 'Login',
      register: 'Register'
    },

    email: {
      label: 'Email Address',
      placeholder: 'Enter your email address'
    },
    password: {
      label: 'Password',
      placeholder: 'Enter your password'
    },
    submit: 'Login',
    dividerText: 'or continue with',
    browseAsGuest: 'Browse as guest',

    forgotPassword: 'Forgot password?',

    register: {
      sendCode: 'Send Code',
      resendCode: 'Resend',
      resendIn: 'Resend in {seconds}s',
      codeSent: 'Verification code sent',
      codeSentTo: 'Code sent to {email}',
      codePlaceholder: 'Enter 6-digit code',
      verify: 'Verify',
      nextStep: 'Next',
      usernameLabel: 'Username',
      usernamePlaceholder: 'Choose a username',
      confirmPasswordLabel: 'Confirm Password',
      confirmPasswordPlaceholder: 'Re-enter your password',
      submit: 'Complete',
      success: 'Registration successful!',
      emailExists: 'This email is already registered',
      rateLimit: 'Too many requests, please try again later'
    },

    forgotPasswordModal: {
      title: 'Reset Password',
      stepSendCode: 'Send Code',
      stepVerify: 'Verify',
      stepReset: 'New Password',
      sendCode: 'Send Code',
      resetButton: 'Reset Password',
      emailNotFound: 'This email is not registered',
      accountDisabled: 'Account has been disabled, password cannot be reset',
      verificationExpired: 'Verification expired, please request a new code',
      resetSuccess: 'Password reset successful, please log in with your new password',
      resetFailed: 'Failed to reset password'
    },

    passwordStrength: {
      weak: 'Weak',
      medium: 'Medium',
      strong: 'Strong',
      hint: 'At least 8 characters with letters and numbers'
    },

    validation: {
      emailRequired: 'Please enter your email',
      passwordRequired: 'Please enter your password',
      passwordMinLength: 'Password must be at least 8 characters',
      passwordPattern: 'Password must contain letters and numbers',
      confirmPasswordRequired: 'Please confirm your password',
      passwordMismatch: 'Passwords do not match',
      codeRequired: 'Please enter the code',
      codeFormat: 'Code must be 6 digits',
      usernameRequired: 'Please enter a username',
      usernameLength: 'Username must be 2-30 characters',
      usernamePattern: 'Only letters, numbers, underscores and Chinese characters'
    },

    messages: {
      loginSuccess: 'Login successful!',
      loginFailed: 'Login failed: {error}',
      registerFailed: 'Registration failed: {error}',
      codeVerified: 'Email verified successfully',
      codeInvalid: 'Invalid or expired code',
      sendCodeFailed: 'Failed to send verification code'
    },

    // Guest login prompt modal (shown when anonymous user clicks a write action)
    guestPrompt: {
      title: 'Login to continue',
      desc: 'Log in to unlock full features and track your browsing history.',
      descWithAction: 'Log in to {action}, save posts, and interact.',
      goLogin: 'Log in',
      goRegister: 'Create an account',
      continueGuest: 'Not now, keep browsing',
      actions: {
        like: 'like posts',
        collect: 'collect posts',
        comment: 'post comments',
        join: 'join circles',
        follow: 'follow people'
      }
    }
  },

  // 表单验证
  validation: {
    required: 'This field is required',
    invalidEmail: 'Please enter a valid email',
    invalidPhone: 'Please enter a valid phone number',
    minLength: 'Minimum {min} characters required',
    maxLength: 'Maximum {max} characters allowed',
    tooShort: 'Content too short',
    tooLong: 'Content too long'
  },

  // 评论相关
  comment: {
    // Post no longer visible (deleted / private circle) → comment create returns 404
    unavailable: 'This content is unavailable or you no longer have access',
    editor: {
      title: 'Post a Comment',
      placeholder: 'Write your comment... (supports images and emoji)',
      submit: 'Post Comment',
      success: 'Comment posted',
      failed: 'Comment failed',
      uploadImage: 'Upload Image',
      loginToComment: 'Log in to post a comment...'
    },
    list: {
      title: 'Comments ({count})',
      viewReplies: 'View {count} replies',
      collapseReplies: 'Collapse replies',
      loadMoreReplies: 'Load more replies',
      repliesTitle: 'Replies ({count})',
      closeReplies: 'Close replies',
      retryLoadReplies: 'Failed to load. Tap to retry',
      noMore: 'No more comments',
      empty: 'No comments yet. Be the first to comment!',
      pagination: {
        total: 'Total {count}',
        page: 'Goto',
        goto: 'Go'
      }
    },
    sort: {
      newest: 'Newest',
      hottest: 'Hottest'
    },
    actions: {
      reply: 'Reply'
    },
    reply: {
      placeholder: '{name}',
      success: 'Reply posted',
      failed: 'Reply failed'
    }
  },

  // In-app notifications (message center)
  notice: {
    title: 'Notifications',
    tabs: {
      all: 'All',
      like: 'Likes',
      collect: 'Favorites',
      comment: 'Comments',
      reply: 'Replies',
      mention: 'Mentions',
      follow: 'Follows',
      review: 'Review'
    },
    readAll: 'Mark all as read',
    empty: 'No notifications',
    noMore: 'No more notifications',
    loadFailed: 'Failed to load notifications',
    readAllSuccess: 'All marked as read',
    readAllFailed: 'Operation failed, please retry',
    targetDeleted: 'Content has been deleted',
    locateFailed: 'Failed to locate the comment, please retry later',
    reportTargetGone: 'This post has been handled or is no longer visible',
    someone: 'Someone',
    // Text templates: {actor}=trigger user, {snippet}=content snippet
    templates: {
      likePost: '{actor} liked your post "{snippet}"',
      likeComment: '{actor} liked your comment "{snippet}"',
      collectPost: '{actor} favorited your post "{snippet}"',
      commentPost: '{actor} commented on your post: {snippet}',
      replyComment: '{actor} replied to your comment: {snippet}',
      mention: '{actor} mentioned you: {snippet}',
      postApproved: 'Your post "{snippet}" has been approved',
      postRejected: 'Your post "{snippet}" didn\'t pass review',
      postReported: 'A post in your circle, "{snippet}", was reported. Please review it.',
      postSelfHarm: 'A post by {actor} in your circle, "{snippet}", may show signs of self-harm. Please check in.',
      postRemoved: 'Your post "{snippet}" was reported and removed after review by a circle owner / admin',
      follow: '{actor} followed you',
      // Follower deleted / disabled (no actor)
      followAnonymous: 'Someone followed you'
    },
    mention: {
      button: 'Mention',
      searchPlaceholder: 'Search users...',
      empty: 'No users found',
      limitTip: 'Mention up to 10 users',
      suggestHint: "Type {'@'} to mention someone"
    }
  },

  // User follow (backend v0.9.0; workstream B appends page copy here)
  follow: {
    // Follow button label by relation (relationLabelKey in utils/follow.js)
    relation: {
      none: 'Follow',
      following: 'Following',
      followedBy: 'Follow back',
      mutual: 'Mutual'
    },
    unfollow: 'Unfollow',
    followingCount: 'Following',
    followerCount: 'Followers',
    listPrivate: 'This user keeps their following and follower lists private',
    empty: {
      following: 'Not following anyone yet',
      followers: 'No followers yet',
      mutual: 'No mutual follows yet'
    },
    settings: {
      groupTitle: 'Privacy',
      listVisibility: 'Show my following and follower lists',
      listVisibilityHint: 'When off, others only see the counts, not the names',
      loadFailed: 'Failed to load settings',
      saved: 'Saved',
      saveFailed: 'Failed to save. Please try again later'
    },
    mutualEntry: 'Mutual',
    you: 'You',
    retry: 'Retry',
    loadFailed: 'Failed to load the list',
    noMore: 'No more',
    listOwner: "{name}'s following and followers",
    followBack: 'Follow back',
    // Error copy: followErrorKey in utils/follow.js
    error: {
      limitReached: 'You have reached the follow limit',
      self: "You can't follow yourself",
      listPrivate: 'This user keeps their following and follower lists private',
      userNotFound: 'User not found',
      loginRequired: 'Please log in first',
      generic: 'Something went wrong. Please try again later'
    }
  },

  // Direct messages (backend v0.9.0; workstream C appends page copy here. Top-level `messages` is generic toasts)
  message: {
    title: 'Messages',
    entry: 'Message',
    send: 'Send',
    inputPlaceholder: 'Write a message…',
    empty: 'No messages yet',
    emptyChat: 'No messages yet. Say hi!',
    deletedUser: 'Deleted user',
    newMessagesDivider: 'New messages',
    loadOlder: 'Load earlier messages',
    deleteConversation: 'Delete conversation',
    deleteConfirm: 'The other person is not affected. If they message you again, the conversation comes back with only the newer messages.',
    // Shown when remaining_count is a number (5-message limit before they follow you)
    remaining: 'You can send {count} more until they follow you or reply',
    newMessageFrom: '{name} sent you a message',
    // Who can message me (DM_PERMISSION_OPTIONS in utils/message.js)
    permission: {
      label: 'Who can message me',
      everyone: 'Everyone',
      following: 'People I follow',
      mutual: 'Mutual follows only',
      closed: 'No one'
    },
    // Cannot send / send failed: dmReasonKey, dmSendErrorKey in utils/message.js
    error: {
      recipientNotFound: 'This user does not exist',
      recipientUnavailable: "This user can't receive messages",
      followingOnly: 'This user only accepts messages from people they follow',
      mutualOnly: 'This user only accepts messages from mutual follows',
      dmClosed: 'This user has turned off messages',
      dmLimitReached: 'You can send more once they follow you or reply',
      senderDisabled: "Your account can't send messages right now",
      conversationNotFound: 'Conversation not found',
      rateLimited: "You're sending too fast. Please try again later",
      contentEmpty: "Message can't be empty",
      contentTooLong: 'Message must be {max} characters or fewer',
      network: 'Failed to send. Check your connection and try again',
      unknown: 'Failed to send. Please try again later'
    }
  },

  // Post sharing (backend v0.9.0; workstream D appends page copy here)
  share: {
    button: 'Share',
    linkCopied: 'Link copied',
    copyFailed: 'Copy failed. Please copy the link manually',
    // Error copy: shareErrorKey in utils/share.js
    error: {
      notFound: "This post can't be shared",
      retry: "Couldn't record the share. Please try again"
    }
  },

  // 图片上传
  upload: {
    uploading: 'Uploading',
    success: 'Upload successful',
    failed: 'Upload failed, please try again',
    maxImages: 'Up to {max} images allowed',
    exceedLimit: 'Limit exceeded, uploading the first {remaining} only'
  },

  // Home feed
  feed: {
    refreshed: 'Recommendations refreshed',
    empty: 'No content yet',
    emptyFollowing: 'Posts from circles you join and people you follow show up here. Join a circle or follow people you find interesting',
    loadFailed: 'Failed to load',
    loginRequiredTab: 'This content requires login to view'
  },

  // Search results page
  report: {
    reasons: {
      spam: 'Spam / ads',
      sexual: 'Sexual content',
      harassment: 'Harassment',
      illegal: 'Illegal activity',
      violence: 'Violence / terrorism',
      privacy: 'Privacy violation',
      selfHarm: 'Self-harm',
      other: 'Other'
    }
  },
  search: {
    emptyKeyword: 'Please enter a search keyword',
    unknownCircle: 'Unknown circle',
    untitled: 'Untitled',
    failed: 'Search failed',
    failedRetry: 'Search failed, please try again later'
  },

  // OAuth success callback page (/auth/success)
  authCallback: {
    welcome: 'Welcome back!',
    processing: 'Processing...',
    enterCommunity: 'Enter Community',
    signingIn: 'Signing in...',
    loginFailed: 'Login failed',
    noLoginInfo: 'No login info detected, redirecting to login page...'
  },

  // 消息提示
  messages: {
    loginRequired: 'Please login first',
    pleaseLoginFirst: 'Please login first',
    operationFailed: 'Operation failed: {error}',
    getDetailFailed: 'Failed to get details: {error}',
    deleteSuccess: 'Deleted successfully',
    deleteFailed: 'Delete failed: {error}',
    updateSuccess: 'Updated successfully',
    updateFailed: 'Update failed: {error}',
    copySuccess: 'Copied successfully',
    copyFailed: 'Copy failed',
    likeFeaturePending: 'Like feature pending',
    favoriteFeaturePending: 'Favorite feature pending',
    interactionRetryLater: "Action didn't go through, please try again later",
    contentUnavailable: 'This content is no longer available',
    likeFailed: 'Like operation failed'
  },

  // Terms of Service
  terms: {
    title: 'User Agreement',
    lastUpdated: 'Last updated: January 2025',
    backToLogin: 'Back to Login',
    sections: [
      {
        title: '1. Acceptance of Terms',
        paragraphs: [
          'Welcome to Qubar (the "Service"). By accessing or using the Service, you confirm that you have read, understood, and agree to be bound by this User Agreement (the "Agreement"). If you do not agree to any of the terms of this Agreement, please do not use the Service.'
        ]
      },
      {
        title: '2. Service Description',
        paragraphs: [
          'Qubar is an interest-based social platform designed to help users discover and connect with various interest communities. The Service includes, but is not limited to:'
        ],
        lists: [
          [
            'User account creation and authentication',
            'Interest tags and community discovery',
            'Interaction and communication with other users',
            'Content sharing and discussion',
            'Personalized recommendations and matching'
          ]
        ]
      },
      {
        title: '3. User Accounts',
        subsections: [
          {
            title: '3.1 Account Registration',
            paragraphs: ['To use the Service, you need to create an account. You represent and warrant that:'],
            lists: [
              [
                'The registration information you provide is true, accurate, and complete',
                'You are at least 13 years old or meet the legal age in your region',
                'You are the sole person using your account',
                'You will maintain the security of your account information'
              ]
            ]
          },
          {
            title: '3.2 Account Security',
            paragraphs: [
              'You are responsible for all activities that occur under your account, whether authorized or not. If you become aware of any unauthorized use of your account, please notify us immediately.'
            ]
          }
        ]
      },
      {
        title: '4. User Conduct',
        paragraphs: ['When using the Service, you agree not to:'],
        lists: [
          [
            'Upload, post, or transmit any illegal, harmful, threatening, abusive, harassing, defamatory, vulgar, obscene, or otherwise objectionable content',
            'Impersonate any person or entity, or falsely state or otherwise misrepresent your affiliation with any person or entity',
            'Upload, post, or transmit any content that infringes any patent, trademark, trade secret, copyright, or other proprietary rights of any party',
            'Interfere with or disrupt the Service or servers and networks connected to the Service',
            'Intentionally or unintentionally violate any applicable local, state, national, or international law',
            'Harass or otherwise harm another user',
            'Collect or store personal data of other users'
          ]
        ]
      },
      {
        title: '5. Intellectual Property',
        paragraphs: [
          'The Service and its original content, features, and design are the exclusive property of Qubar and its licensors, protected by copyright, trademark, and other intellectual property laws. You agree not to copy, modify, distribute, or otherwise use any part of the Service without our express written permission.'
        ]
      },
      {
        title: '6. Content Rights',
        paragraphs: [
          'You retain ownership of all content you submit, post, or display through the Service ("Your Content"). By submitting, posting, or displaying Your Content, you grant us a worldwide, royalty-free, non-exclusive license to use, copy, modify, distribute, and display Your Content in order to:'
        ],
        lists: [
          [
            'Provide, maintain, and improve the Service',
            'Develop and provide new services',
            'Prevent abuse and illegal activities',
            'Fulfill our legal obligations'
          ]
        ]
      },
      {
        title: '7. Termination',
        paragraphs: [
          'We may, at our sole discretion and at any time, with or without notice, suspend or terminate your account and right to access the Service, for reasons including but not limited to:'
        ],
        lists: [
          [
            'Violation of this Agreement',
            'Engaging in fraudulent or illegal activities',
            'Posing a security risk to other users or our service',
            'Prolonged inactivity'
          ]
        ]
      },
      {
        title: '8. Disclaimer',
        paragraphs: [
          'The Service is provided on an "as is" and "as available" basis, without any express or implied warranties. We are not liable for:'
        ],
        lists: [
          [
            'The continuous availability, timeliness, or security of the Service',
            'The accuracy, reliability, or content of any content, advertising, products, or other materials included in or linked from the Service',
            'Any direct, indirect, incidental, special, or consequential damages arising from the use of the Service'
          ]
        ]
      },
      {
        title: '9. Indemnification',
        paragraphs: [
          'You agree to indemnify and hold Qubar and its officers, directors, employees, agents, and licensors harmless from any claims, damages, obligations, losses, liabilities, costs, or expenses arising from your use of the Service or your breach of this Agreement.'
        ]
      },
      {
        title: '10. Changes to the Agreement',
        paragraphs: [
          'We reserve the right to modify or replace this Agreement at any time. If the revisions are material, we will provide notice before the new terms take effect. Continued use of the Service constitutes your acceptance of the revised Agreement.'
        ]
      },
      {
        title: '11. Governing Law',
        paragraphs: [
          'This Agreement is governed by and construed in accordance with the laws of the People\'s Republic of China. Any disputes arising from this Agreement shall be submitted to a court with jurisdiction for resolution.'
        ]
      },
      {
        title: '12. Contact Us',
        paragraphs: ['If you have any questions or concerns about this Agreement, please contact us at:'],
        lists: [['Email: legal@qubar.com', 'Address: Pudong New Area, Shanghai, China']]
      }
    ]
  },

  // Privacy Policy
  privacy: {
    title: 'Privacy Policy',
    lastUpdated: 'Last updated: January 2025',
    backToLogin: 'Back to Login',
    intro:
      'Qubar ("we", "us", or "the Service") respects your privacy and is committed to protecting your personal information. This Privacy Policy explains how we collect, use, disclose, and protect your information. By using the Service, you agree to the practices described in this Privacy Policy.',
    sections: [
      {
        title: '1. Information We Collect',
        subsections: [
          {
            title: '1.1 Information You Provide',
            paragraphs: ['When you use the Service, we may collect the following information:'],
            lists: [
              [
                { strong: 'Account information: ', text: 'name, email address, profile picture, date of birth' },
                { strong: 'Authentication information: ', text: 'information provided when authenticating via third-party services (e.g., Google)' },
                { strong: 'Interest tags: ', text: 'your selected or generated interests and tags' },
                { strong: 'Content: ', text: 'posts, comments, images, videos, and other content you publish' },
                { strong: 'Communications: ', text: 'correspondence between you and us' }
              ]
            ]
          },
          {
            title: '1.2 Automatically Collected Information',
            paragraphs: ['When you access or use the Service, we automatically collect:'],
            lists: [
              [
                { strong: 'Device information: ', text: 'device type, operating system, browser type, unique device identifier' },
                { strong: 'Usage information: ', text: 'access time, pages viewed, features used, links clicked' },
                { strong: 'Location information: ', text: 'approximate geographic location based on IP address' },
                { strong: 'Log information: ', text: 'server logs, error reports, performance data' }
              ]
            ]
          },
          {
            title: '1.3 Cookies and Similar Technologies',
            paragraphs: [
              'We use cookies, web beacons, and similar technologies to collect and store information. Cookies are small text files stored on your device to remember your preferences, improve user experience, and analyze usage trends.'
            ]
          }
        ]
      },
      {
        title: '2. How We Use Your Information',
        paragraphs: ['We use the collected information to:'],
        lists: [
          [
            { strong: 'Provide, maintain, and improve the service: ', text: 'process your requests, personalize your experience, develop new features' },
            { strong: 'Authentication and security: ', text: 'verify your identity, prevent fraud and abuse, protect account security' },
            { strong: 'Personalized recommendations: ', text: 'recommend content, users, and communities based on your interests' },
            { strong: 'Communication: ', text: 'send service notifications, updates, security alerts, and marketing messages (optional)' },
            { strong: 'Analytics: ', text: 'analyze usage trends, improve service performance, understand user behavior' },
            { strong: 'Legal compliance: ', text: 'comply with legal obligations, respond to legal requests, protect our rights' }
          ]
        ]
      },
      {
        title: '3. Information Sharing',
        subsections: [
          {
            title: '3.1 We Do Not Sell Your Personal Information',
            paragraphs: ['We do not sell your personal information to third parties.']
          },
          {
            title: '3.2 Circumstances Under Which We May Share Information',
            lists: [
              [
                { strong: 'Service providers: ', text: 'sharing necessary information with third-party service providers that help us operate the Service (e.g., cloud storage, data analytics, payment processing)' },
                { strong: 'Business transfers: ', text: 'in the event of a merger, acquisition, or asset sale, your information may be transferred to the relevant party' },
                { strong: 'Legal requirements: ', text: 'disclosing information when we believe in good faith that the law requires it, to protect our rights, or to prevent illegal activities' },
                { strong: 'With your consent: ', text: 'sharing information in other ways with your explicit consent' }
              ]
            ]
          },
          {
            title: '3.3 Public Information',
            paragraphs: [
              'Your profile information (name, photo, interest tags) is visible to other users. You can control what information is public through your account settings.'
            ]
          }
        ]
      },
      {
        title: '4. Data Security',
        paragraphs: ['We take reasonable technical and organizational measures to protect your information from unauthorized access, use, or disclosure:'],
        lists: [
          [
            { strong: 'Encryption: ', text: 'SSL/TLS encryption during transmission' },
            { strong: 'Access control: ', text: 'restricting access to personal information' },
            { strong: 'Security audits: ', text: 'conducting regular security reviews and assessments' },
            { strong: 'Data retention: ', text: 'retaining information for the necessary period, then securely deleting it' }
          ]
        ],
        warning:
          'While we strive to protect your information, no method of internet transmission or electronic storage is 100% secure. Although we seek to protect your personal information with reasonable means, we cannot guarantee its absolute security.'
      },
      {
        title: '5. Your Privacy Rights',
        paragraphs: ['Under applicable law, you may have the following rights:'],
        lists: [
          [
            { strong: 'Right of access: ', text: 'request a copy of the personal information we hold about you' },
            { strong: 'Right to rectification: ', text: 'request correction of inaccurate or incomplete information' },
            { strong: 'Right to erasure: ', text: 'request deletion of your personal information' },
            { strong: 'Right to restrict processing: ', text: 'request that we limit how we use your information' },
            { strong: 'Right to data portability: ', text: 'request to receive your information in a structured format' },
            { strong: 'Right to object: ', text: 'object to our processing of your information' },
            { strong: 'Withdraw consent: ', text: 'withdraw consent previously given (without affecting legality prior to withdrawal)' }
          ]
        ],
        paragraphs2: ['To exercise these rights, please contact us using the contact details at the bottom of this Policy. We will respond to your request promptly.']
      },
      {
        title: '6. Third-Party Links',
        paragraphs: [
          'The Service may contain links to third-party websites or services. We are not responsible for the practices or content of third-party websites. We encourage you to read the privacy policies of these third parties when you leave our Service.'
        ]
      },
      {
        title: '7. Children\'s Privacy',
        paragraphs: [
          'The Service is not directed at children under 13. We do not knowingly collect personal information from children under 13. If we discover that we have collected such information, we will take steps to delete it.'
        ]
      },
      {
        title: '8. Cross-Border Data Transfer',
        paragraphs: [
          'Your information may be transferred to and processed in countries or regions outside your own. The data protection laws of these countries or regions may differ from those in your country or region.',
          'When we transfer your information across borders, we will take appropriate measures to ensure your information receives protection consistent with this Privacy Policy.'
        ]
      },
      {
        title: '9. Changes to the Privacy Policy',
        paragraphs: [
          'We may update this Privacy Policy from time to time. After changes, we will post the new Privacy Policy on this page and update the "Last updated" date. We recommend that you review this Privacy Policy periodically.',
          'Material changes will be communicated via email or prominent notices within the Service. Continued use of the Service constitutes your acceptance of the updated Privacy Policy.'
        ]
      },
      {
        title: '10. Contact Us',
        paragraphs: ['If you have any questions, concerns, or requests regarding this Privacy Policy or our information practices, please contact us at:'],
        lists: [
          [
            { strong: 'Email: ', text: 'privacy@qubar.com' },
            { strong: 'Address: ', text: 'Zhangjiang Hi-Tech Park, Pudong New Area, Shanghai, China' },
            { strong: 'Phone: ', text: '+86 21-1234-5678' }
          ]
        ],
        paragraphs2: ['Data Protection Officer contact:', 'Email: dpo@qubar.com']
      },
      {
        title: '11. Governing Law',
        paragraphs: [
          'This Privacy Policy is governed by and construed in accordance with the laws of the People\'s Republic of China. We comply with applicable data protection laws, including the Personal Information Protection Law (PIPL) and the Cybersecurity Law of the People\'s Republic of China.'
        ]
      }
    ],
    summaryTitle: 'Summary of Privacy Principles',
    summary: [
      '✓ Transparency: We clearly explain how we use your data',
      '✓ Control: You have rights to control your personal information',
      '✓ Security: We implement strong security measures to protect your information',
      '✓ Compliance: We comply with global privacy regulations',
      '✓ Minimization: We only collect necessary information'
    ]
  }
}
